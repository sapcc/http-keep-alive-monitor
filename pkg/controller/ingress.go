package controller

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/http-keep-alive-monitor/pkg/keepalive"
	corev1 "k8s.io/api/core/v1"
	netv1beta1 "k8s.io/api/networking/v1beta1"
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
		[]string{"ingress", "ingress_namespace"},
	)
	httpKeepaliveErrorsCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "http_keepalive",
			Name:      "errors_total",
			Help:      "errors that happend while measuring the timeout",
		},
		[]string{"ingress", "ingress_namespace"},
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

// Implement the business logic:
// This function will be called when there is a change to a ReplicaSet or a Pod with an OwnerReference
// to a ReplicaSet.
//
// * Read the ReplicaSet
// * Read the Pods
// * Set a Label on the ReplicaSet with the Pod count
func (a *IngressReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {

	log := logf.FromContext(ctx)
	ing := &netv1beta1.Ingress{}
	err := a.Get(ctx, req.NamespacedName, ing)
	if client.IgnoreNotFound(err) != nil {
		return reconcile.Result{}, err
	}
	if apierrors.IsNotFound(err) || ing.DeletionTimestamp != nil {
		a.delete(req.NamespacedName)
		return reconcile.Result{}, nil
	}
	log.Info("Reconciling", "class", ing.Spec.IngressClassName)

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
	labels := prometheus.Labels{"ingress": key.Name, "ingress_namespace": key.Namespace}
	httpKeepaliveIdleTimeout.Delete(labels)
	httpKeepaliveErrorsCount.Delete(labels)
	if a.monitors == nil {
		return
	}
	cancel, activeMonitor := a.monitors[key]
	if activeMonitor {
		delete(a.monitors, key)
		cancel()
	}
}

func deleteMetrics(key types.NamespacedName) {
	labels := prometheus.Labels{"ingress": key.Name, "ingress_namespace": key.Namespace}
	httpKeepaliveIdleTimeout.Delete(labels)
	httpKeepaliveErrorsCount.Delete(labels)

}

func monitor(key types.NamespacedName, client client.Client, timeout time.Duration) func(context.Context) {
	return func(ctx context.Context) {
		log := logf.FromContext(ctx)

		ing := &netv1beta1.Ingress{}
		err := client.Get(ctx, key, ing)
		if err != nil {
			log.Info("Failed to probe", "err", err)
			return
		}
		var backends = map[string]struct{}{}
		if ing.Spec.Backend != nil {
			address, err := resolveBackend(ctx, client, ing.Namespace, ing.Spec.Backend)
			if err != nil {
				log.Info("Failed to resolve default backend", "backend", ing.Spec.Backend.ServiceName, "err", err)
			} else {

				backends[address] = struct{}{}
			}
		}
		for _, rule := range ing.Spec.Rules {
			if rule.HTTP != nil {
				if len(rule.HTTP.Paths) > 0 {
					for _, r := range rule.HTTP.Paths {
						address, err := resolveBackend(ctx, client, ing.Namespace, &r.Backend)
						if err != nil {
							log.Info("Failed to resolve backend", "err", err, "backend", r.Backend.ServiceName)
							continue
						}
						backends[address] = struct{}{}
					}
				}
			}
		}
		if len(backends) > 1 {
			log.Info("Warning. More then one backend found. Picking one randomly")
		}
		labels := prometheus.Labels{"ingress": key.Name, "ingress_namespace": key.Namespace}
		for address, _ := range backends {
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

			log.Info("Probing", "address", address)
			return
		}

	}
}

func resolveBackend(ctx context.Context, c client.Client, namespace string, backend *netv1beta1.IngressBackend) (string, error) {
	svc := &corev1.Service{}
	err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: backend.ServiceName}, svc)
	if err != nil {
		return "", fmt.Errorf("Failed to get service: %w", err)
	}
	host := svc.Spec.ClusterIP
	if host == corev1.ClusterIPNone {
		host = fmt.Sprintf("%s.%s.svc.cluster.local", svc.Name, svc.Namespace)
	}

	if backend.ServicePort.IntValue() > 0 {
		return fmt.Sprintf("%s:%d", host, backend.ServicePort.IntValue()), nil
	}

	for _, port := range svc.Spec.Ports {
		if port.Name == backend.ServicePort.String() {
			return fmt.Sprintf("%s:%d", host, port.Port), nil
		}
	}
	return "", fmt.Errorf("Port %s not found on service", backend.ServicePort)

}
