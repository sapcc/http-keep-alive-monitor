package main

import (
	"flag"
	"os"
	"time"

	"github.com/sapcc/http-keep-alive-monitor/pkg/controller"
	netv1beta1 "k8s.io/api/networking/v1beta1"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
)

func main() {
	var kubecontext, metricsAddr, ingressClass string
	var idleTimeout time.Duration
	var skipNoClass bool
	flag.StringVar(&kubecontext, "kubecontext", "", "context to use from kubeconfig (env: KUBECONTEXT, U8S_CONTEXT, default: current-context)")
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.DurationVar(&idleTimeout, "idle-timeout", 61*time.Second, "Global timeout used when probing services")
	flag.BoolVar(&skipNoClass, "skip-no-class", false, "Ignore ingress resources without an explicit ingress class")
	flag.StringVar(&ingressClass, "ingress-class", "", "Restrict to ingress resources of given ingress class")

	flag.Parse()

	logf.SetLogger(zap.New(func(o *zap.Options) {
		o.Development = true
	}))

	var log = logf.Log.WithName("bootstrap")

	if kubecontext == "" {
		kubecontext = os.Getenv("KUBECONTEXT")
	}
	if kubecontext == "" {
		kubecontext = os.Getenv("U8S_CONTEXT")
	}
	c, err := config.GetConfigWithContext(kubecontext)
	if err != nil {
		log.Error(err, "could not create kubeclient")
		os.Exit(1)
	}

	mgr, err := manager.New(c, manager.Options{
		MetricsBindAddress: metricsAddr,
	})
	if err != nil {
		log.Error(err, "could not create manager")
		os.Exit(1)
	}

	err = builder.
		ControllerManagedBy(mgr).   // Create the ControllerManagedBy
		For(&netv1beta1.Ingress{}). // Watch Ingress definitions
		Complete(&controller.IngressReconciler{
			KeepAliveTimeout: idleTimeout,
			DefaultClass:     !skipNoClass,
			IngressClass:     ingressClass,
		})
	if err != nil {
		log.Error(err, "could not create controller")
		os.Exit(1)
	}

	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		log.Error(err, "could not start manager")
		os.Exit(1)
	}
}
