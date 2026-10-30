// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package certroll

import (
	"context"
	"time"

	"go.uber.org/zap"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
)

const (
	Debounce = 10 * time.Second
	resync   = 10 * time.Minute
)

// Blocks until ctx ends. Informer events only wake the loop; the fingerprint compare decides whether anything rolls.
func (r *Roller) Run(ctx context.Context, namespaces []string, debounce time.Duration) {
	wake := make(chan struct{}, 1)
	for _, namespace := range namespaces {
		if err := r.watchNamespace(ctx, namespace, wake); err != nil {
			r.log.Error("certroll: add handler", zap.String("namespace", namespace), zap.Error(err))
			return
		}
	}
	r.loop(ctx, namespaces, wake, debounce)
}

func (r *Roller) watchNamespace(ctx context.Context, namespace string, wake chan<- struct{}) error {
	poke := func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	informer := dynamicinformer.NewFilteredDynamicInformer(r.dyn, certificates, namespace, resync, cache.Indexers{}, nil).Informer()
	_, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(any) { poke() },
		UpdateFunc: func(any, any) { poke() },
	})
	if err == nil {
		go informer.Run(ctx.Done())
	}
	return err
}

func (r *Roller) loop(ctx context.Context, namespaces []string, wake <-chan struct{}, debounce time.Duration) {
	for awaitSettled(ctx, wake, debounce) {
		for _, namespace := range namespaces {
			r.reconcileLogged(ctx, namespace)
		}
	}
}

func (r *Roller) reconcileLogged(ctx context.Context, namespace string) {
	if err := r.Reconcile(ctx, namespace); err != nil {
		r.log.Error("certroll: reconcile failed", zap.String("namespace", namespace), zap.Error(err))
	}
}

func awaitSettled(ctx context.Context, wake <-chan struct{}, debounce time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-wake:
		return quiet(ctx, wake, debounce)
	}
}

func quiet(ctx context.Context, wake <-chan struct{}, window time.Duration) bool {
	timer := time.NewTimer(window)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-wake:
			timer.Reset(window)
		case <-timer.C:
			return true
		}
	}
}
