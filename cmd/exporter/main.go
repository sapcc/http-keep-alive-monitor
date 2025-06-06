// SPDX-FileCopyrightText: 2021 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/sapcc/http-keep-alive-monitor/pkg/controller"

	"github.com/sapcc/go-api-declarations/bininfo"
	netv1 "k8s.io/api/networking/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
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
	flag.BoolFunc("version", "Show version information", func(_ string) error {
		fmt.Print(bininfo.Version())
		os.Exit(0)
		return nil
	})

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
		Metrics: server.Options{
			BindAddress: metricsAddr,
		},
	})
	if err != nil {
		log.Error(err, "could not create manager")
		os.Exit(1)
	}

	err = builder.
		ControllerManagedBy(mgr). // Create the ControllerManagedBy
		For(&netv1.Ingress{}).    // Watch Ingress definitions
		Complete(&controller.IngressReconciler{
			Client:           mgr.GetClient(),
			DefaultClass:     !skipNoClass,
			IngressClass:     ingressClass,
			KeepAliveTimeout: idleTimeout,
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
