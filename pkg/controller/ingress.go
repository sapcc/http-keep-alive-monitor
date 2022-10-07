package controller

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/sapcc/http-keep-alive-monitor/pkg/keepalive"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	httpKeepaliveIdleTimeout = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "http_keepalive",
			Name:      "idle_timeout_seconds",
			Help:      "the idle timeout measured for http keepalive connectiosn",
		},
		[]string{"ingress", "ingress_namespace", "backend"},
	)
	httpKeepaliveErrorsCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "http_keepalive",
			Name:      "errors_total",
			Help:      "errors that happend while measuring the timeout",
		},
		[]string{"ingress", "ingress_namespace", "backend"},
	)
)

func init() {
	metrics.Registry.Register(httpKeepaliveIdleTimeout)
	metrics.Registry.Register(httpKeepaliveErrorsCount)
}

// ReplicaSetReconciler is a simple ControllerManagedBy example implementation.
type IngressReconciler struct {
	client.Client
	IngressClass     string
	DefaultClass     bool
	KeepAliveTimeout time.Duration

	monitors map[types.NamespacedName]func()
	mu       sync.Mutex
}

const (
	NoKeepAliveMonitorAnnotation = "cloud.sap/no-http-keep-alive-monitor"
)

// Implement the business logic:
// This function will be called when there is a change to a ReplicaSet or a Pod with an OwnerReference
// to a ReplicaSet.
//
// * Read the ReplicaSet
// * Read the Pods
// * Set a Label on the ReplicaSet with the Pod count
func (a *IngressReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {

	log := logf.FromContext(ctx)
	ing := &netv1.Ingress{}
	err := a.Get(ctx, req.NamespacedName, ing)
	if client.IgnoreNotFound(err) != nil {
		return reconcile.Result{}, err
	}
	if apierrors.IsNotFound(err) || ing.DeletionTimestamp != nil {
		a.delete(req.NamespacedName)
		return reconcile.Result{}, nil
	}
	log.Info("Reconciling", "class", ing.Spec.IngressClassName)

	if _, ok := ing.Annotations[NoKeepAliveMonitorAnnotation]; ok {
		log.Info(fmt.Sprintf("Skipping resource with %s annotation", NoKeepAliveMonitorAnnotation))
		a.delete(req.NamespacedName)
		return reconcile.Result{}, nil
	}

	ingressClass := ing.Annotations["kubernetes.io/ingress.class"]
	if ing.Spec.IngressClassName != nil {
		ingressClass = *ing.Spec.IngressClassName
	}

	//Ignore ingress resources without class
	if !a.DefaultClass && ingressClass == "" {
		log.Info("Skipping resource with no class")
		a.delete(req.NamespacedName)
		return reconcile.Result{}, nil
	}
	//Ignore ingress of non matching ingress class
	if a.IngressClass != "" && ingressClass != "" && ingressClass != a.IngressClass {
		log.Info("Skipping resource with non-matching class", "want", a.IngressClass, "have", ingressClass)
		a.delete(req.NamespacedName)
		return reconcile.Result{}, nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.monitors == nil {
		a.monitors = make(map[types.NamespacedName]func())
	}
	//create missing monitor
	if _, exists := a.monitors[req.NamespacedName]; !exists {
		c, cancelFn := context.WithCancel(ctx)
		go wait.JitterUntilWithContext(c, monitor(req.NamespacedName, a.Client, a.KeepAliveTimeout), a.KeepAliveTimeout, 0.0, false)
		a.monitors[req.NamespacedName] = cancelFn
	}

	return reconcile.Result{}, nil
}

func (a *IngressReconciler) InjectClient(c client.Client) error {
	a.Client = c
	return nil
}

func (a *IngressReconciler) delete(key types.NamespacedName) {
	a.mu.Lock()
	defer a.mu.Unlock()

	defer deleteMetrics(httpKeepaliveIdleTimeout, key)
	defer deleteMetrics(httpKeepaliveErrorsCount, key)
	if a.monitors == nil {
		return
	}
	cancel, activeMonitor := a.monitors[key]
	if activeMonitor {
		delete(a.monitors, key)
		cancel()
	}
}

type PromVec interface {
	Collect(chan<- prometheus.Metric)
	Delete(prometheus.Labels) bool
}

func deleteMetrics(metricVec PromVec, key types.NamespacedName) {

	ch := make(chan prometheus.Metric)
	endCh := make(chan struct{})
	go func() {
		metricVec.Collect(ch)
		close(endCh)
	}()
	m := new(dto.Metric)

	for {
		select {
		case metric := <-ch:
			if err := metric.Write(m); err != nil {
				continue
			}
			labels := prometheus.Labels{}
			for _, p := range m.GetLabel() {
				labels[p.GetName()] = p.GetValue()
			}

			if labels["ingress"] == key.Name && labels["ingress_namespace"] == key.Namespace {
				// We can't delete metrics while Collecting them without ending up in a dead-lock,
				// so we delete them after Collect has returned
				defer metricVec.Delete(labels)
			}

		case <-endCh:
			return
		}
	}

}

func monitor(key types.NamespacedName, client client.Client, timeout time.Duration) func(context.Context) {
	return func(ctx context.Context) {
		log := logf.FromContext(ctx)

		ing := &netv1.Ingress{}
		err := client.Get(ctx, key, ing)
		if err != nil {
			log.Info("Failed to probe", "err", err)
			return
		}
		var backends = map[string]string{}
		if ing.Spec.DefaultBackend != nil {
			address, svcNameAndPort, err := resolveBackend(ctx, client, ing.Namespace, ing.Spec.DefaultBackend)
			if err != nil {
				log.Info("Failed to resolve default backend", "backend", ing.Spec.DefaultBackend.Service.Name, "err", err)
			} else {

				backends[address] = svcNameAndPort
			}
		}
		for _, rule := range ing.Spec.Rules {
			if rule.HTTP != nil {
				if len(rule.HTTP.Paths) > 0 {
					for _, r := range rule.HTTP.Paths {
						address, svcNameAndPort, err := resolveBackend(ctx, client, ing.Namespace, &r.Backend)
						if err != nil {
							log.Info("Failed to resolve backend", "err", err)
							continue
						}
						backends[address] = svcNameAndPort
					}
				}
			}
		}

		var wg sync.WaitGroup
		for address, svcAndPort := range backends {
			wg.Add(1)
			go func() {
				labels := prometheus.Labels{"ingress": key.Name, "ingress_namespace": key.Namespace, "backend": svcAndPort}
				defer wg.Done()
				dur, _, err := keepalive.MeasureTimeout(url.URL{Scheme: "http", Host: address}, timeout)
				select {
				case <-ctx.Done():
					return // monitor was canceled, no updates
				default:
				}
				if err == nil {
					httpKeepaliveIdleTimeout.With(labels).Set(dur.Seconds())
				} else {
					log.Info("Probing keepalive timeout failed", "err", err)
					httpKeepaliveErrorsCount.With(labels).Add(1)
					httpKeepaliveIdleTimeout.With(labels).Set(-1)
				}
			}()
		}
		wg.Wait()

	}
}

func resolveBackend(ctx context.Context, c client.Client, namespace string, backend *netv1.IngressBackend) (string, string, error) {
	svc := &corev1.Service{}
	if backend.Service == nil {
		return "", "", errors.New("ingress backend does not contain a service reference")
	}
	err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: backend.Service.Name}, svc)
	if err != nil {
		return "", "", fmt.Errorf("Failed to get service: %w", err)
	}
	host := svc.Spec.ClusterIP
	if host == corev1.ClusterIPNone {
		host = fmt.Sprintf("%s.%s.svc.cluster.local", svc.Name, svc.Namespace)
	}

	if backend.Service.Port.Number > 0 {
		return fmt.Sprintf("%s:%d", host, backend.Service.Port.Number), fmt.Sprintf("%s:%d", svc.Name, backend.Service.Port.Number), nil
	}

	for _, port := range svc.Spec.Ports {
		if port.Name == backend.Service.Port.Name {
			return fmt.Sprintf("%s:%d", host, port.Port), fmt.Sprintf("%s:%d", svc.Name, port.Port), nil
		}
	}
	return "", "", fmt.Errorf("Port %s not found on service", backend.Service.Port.Name)

}
