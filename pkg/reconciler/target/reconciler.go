/*
Copyright 2021 NDD.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package target

import (
	"context"
	"time"

	"github.com/pkg/errors"
	nddv1 "github.com/yndd/ndd-runtime/apis/common/v1"
	"github.com/yndd/ndd-runtime/pkg/event"
	"github.com/yndd/ndd-runtime/pkg/logging"
	"github.com/yndd/ndd-runtime/pkg/meta"
	"github.com/yndd/ndd-runtime/pkg/resource"
	targetv1 "github.com/yndd/target/apis/target/v1"
	"github.com/yndd/target/internal/targetchannel"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	// timers
	targetFinalizerName  = "target.yndd.io/finalizer"
	reconcileGracePeriod = 30 * time.Second
	reconcileTimeout     = 1 * time.Minute
	shortWait            = 30 * time.Second
	mediumWait           = 1 * time.Minute
	veryShortWait        = 1 * time.Second
	longWait             = 1 * time.Minute
	defaultpollInterval  = 1 * time.Minute
	defaultGnmiTimeout   = 5 * time.Second

	// errors
	errGetTarget         = "cannot get target cr"
	errUpdateStatus      = "cannot update status of the target cr"
	errCredentials       = "invalid credentials"
	errReconcileConnect  = "cannot connect to external connector"
	errReconcileObserve  = "cannot observe external resource"
	errReconcileCreate   = "cannot create external resource"
	errReconcileDelete   = "cannot delete external resource"
	errHasOtherFinalizer = "cannot get other finalizer"

	//event
	reasonSync          event.Reason = "SyncTarget"
	reasonCannotConnect event.Reason = "CannotConnectToConnector"
	reasonCannotObserve event.Reason = "CannotObserveExternalResource"
	reasonCannotCreate  event.Reason = "CannotCreateExternalResource"
	reasonCannotDelete  event.Reason = "CannotDeleteExternalResource"
	reasonDeleted       event.Reason = "DeletedExternalResource"
	reasonCreated       event.Reason = "CreatedExternalResource"
)

// A Reconciler reconciles target resources by creating and managing the
// lifecycle of a target
type Reconciler struct {
	// config info
	address            string
	expectedVendorType targetv1.VendorType
	//newTarget          func() targetv1.Tg
	targetCh chan targetchannel.TargetMsg

	// target models
	//m  *model.Model
	//fm *model.Model

	// k8s api
	client    client.Client
	finalizer resource.Finalizer

	// timers
	pollInterval time.Duration
	timeout      time.Duration

	// connector for the gnmi client
	//gnmiConnector GnmiConnecter

	log    logging.Logger
	record event.Recorder
}

// A ReconcilerOption configures a Reconciler.
type ReconcilerOption func(*Reconciler)

// WithTargetAddress specifies the address of the gnmi server within the operator
func WithAddress(a string) ReconcilerOption {
	return func(r *Reconciler) {
		r.address = a
	}
}

// WithExpectedVendorType specifies the vendorType the reconciler cares about
func WithExpectedVendorType(t targetv1.VendorType) ReconcilerOption {
	return func(r *Reconciler) {
		r.expectedVendorType = t
	}
}

// WithTimeout specifies the timeout duration cumulatively for all the calls happen
// in the reconciliation function. In case the deadline exceeds, reconciler will
// still have some time to make the necessary calls to report the error such as
// status update.
func WithTimeout(duration time.Duration) ReconcilerOption {
	return func(r *Reconciler) {
		r.timeout = duration
	}
}

// WithPollInterval specifies how long the Reconciler should wait before queueing
// a new reconciliation after a successful reconcile. The Reconciler requeues
// after a specified duration when it is not actively waiting for an external
// operation, but wishes to check whether an existing external resource needs to
// be synced to its ndd Managed resource.
func WithPollInterval(after time.Duration) ReconcilerOption {
	return func(r *Reconciler) {
		r.pollInterval = after
	}
}

// WithLogger specifies how the Reconciler logs messages.
func WithLogger(l logging.Logger) ReconcilerOption {
	return func(r *Reconciler) {
		r.log = l
	}
}

// WithRecorder specifies how the Reconciler records events.
func WithRecorder(er event.Recorder) ReconcilerOption {
	return func(r *Reconciler) {
		r.record = er
	}
}

func WithTargetChannel(tc chan targetchannel.TargetMsg) ReconcilerOption {
	return func(r *Reconciler) {
		r.targetCh = tc
	}
}

// NewReconciler returns a Reconciler that reconciles target resources
func NewReconciler(m manager.Manager, o ...ReconcilerOption) *Reconciler {
	/*
		tg := func() targetv1.Tg {
			return resource.MustCreateObject(schema.GroupVersionKind(targetv1.TargetGroupVersionKind), m.GetScheme()).(targetv1.Tg)
		}
	*/

	// Panic early if we've been asked to reconcile a resource kind that has not
	// been registered with our controller manager's scheme.
	//_ = tg()

	/*
		tfm := &model.Model{
			StructRootType:  reflect.TypeOf((*ygotnddtarget.Device)(nil)),
			SchemaTreeRoot:  ygotnddtarget.SchemaTree["Device"],
			JsonUnmarshaler: ygotnddtarget.Unmarshal,
			EnumData:        ygotnddtarget.ΛEnum,
		}
	*/

	/*
		tm := &model.Model{
			StructRootType:  reflect.TypeOf((*ygotnddtarget.NddTarget_TargetEntry)(nil)),
			SchemaTreeRoot:  ygotnddtarget.SchemaTree["NddTarget_TargetEntry"],
			JsonUnmarshaler: ygotnddtarget.Unmarshal,
			EnumData:        ygotnddtarget.ΛEnum,
		}
	*/

	r := &Reconciler{
		client: m.GetClient(),
		//newTarget: tg,
		//fm:           tfm,
		//m:            tm,
		pollInterval: defaultpollInterval,
		timeout:      reconcileTimeout,
		log:          logging.NewNopLogger(),
		record:       event.NewNopRecorder(),
		finalizer:    resource.NewAPIFinalizer(m.GetClient(), targetFinalizerName),
	}

	for _, ro := range o {
		ro(r)
	}

	/*
		r.gnmiConnector = &connector{
			log: r.log,
			m:   tm,
			//fm:          tfm,
			newClientFn: target.NewTarget,
		}
	*/
	return r
}

// Reconcile a managed resource with an external resource.
func (r *Reconciler) Reconcile(_ context.Context, req reconcile.Request) (reconcile.Result, error) { // nolint:gocyclo
	log := r.log.WithValues("request", req)
	log.Debug("Target", "NameSpaceName", req.NamespacedName)

	ctx, cancel := context.WithTimeout(context.Background(), r.timeout+reconcileGracePeriod)
	defer cancel()

	t := &targetv1.Target{}
	if err := r.client.Get(ctx, req.NamespacedName, t); err != nil {
		// There's no need to requeue if we no longer exist. Otherwise we'll be
		// requeued implicitly because we return an error.
		log.Debug(errGetTarget, "error", err)
		return reconcile.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetTarget)
	}

	//record := r.record.WithAnnotations("external-name", meta.GetExternalName(t))
	log = log.WithValues(
		"uid", t.GetUID(),
		"version", t.GetResourceVersion(),
	)

	// if expectedVendorType is unset we dont care about it and can proceed,
	// if it is set we should see if the Target CR vendor type matches the
	// expected vendorType
	if r.expectedVendorType != targetv1.VendorTypeUnknown {
		// expected vendor type is set, so we compare expected and configured vendor Type

		// if the expected vendor type does not match we return as the CR is not
		// relevant to proceed
		if r.expectedVendorType != t.Spec.Properties.VendorType {
			log.Debug("unexpected vendor type", "crVendorType", t.Spec.Properties.VendorType, "expectedVendorType", r.expectedVendorType)
			// if the spec is no longer related to the vendor we want to remove the discovery information
			if t.Status.DiscoveryInfo != nil && t.Status.DiscoveryInfo.VendorType == r.expectedVendorType {
				t.SetConditions(nddv1.Unknown())
				t.SetDiscoveryInfo(nil)
				return reconcile.Result{Requeue: true}, errors.Wrap(r.client.Status().Update(ctx, t), errUpdateStatus)
			}
			// stop the reconcile process as we should not be processing this cr; the vendor type is not expected
			return reconcile.Result{}, nil
		}
	}

	if meta.WasDeleted(t) {
		// stop/delete target
		r.targetCh <- targetchannel.TargetMsg{
			Target:    req.NamespacedName.String(),
			Operation: targetchannel.Stop,
		}

		// when there is no target and we were requested to be deleted we can remove the
		// finalizer since the target is no longer there and we assume cleanup will happen
		// during target delete/create
		if err := r.finalizer.RemoveFinalizer(ctx, t); err != nil {
			// If this is the first time we encounter this issue we'll be
			// requeued implicitly when we update our status with the new error
			// condition. If not, we requeue explicitly, which will trigger
			// backoff.
			log.Debug("Cannot remove managed resource finalizer", "error", err)
			t.SetConditions(nddv1.ReconcileError(err), nddv1.Unknown())
			//t.SetDiscoveryInfo(nil)
			return reconcile.Result{Requeue: true}, errors.Wrap(r.client.Status().Update(ctx, t), errUpdateStatus)
		}

		// We've successfully deleted our external resource (if necessary) and
		// removed our finalizer. If we assume we were the only controller that
		// added a finalizer to this resource then it should no longer exist and
		// thus there is no point trying to update its status.
		// log.Debug("Successfully deleted managed resource")
		return reconcile.Result{Requeue: false}, nil
	}

	// Add a finalizer to newly created objects
	if err := r.finalizer.AddFinalizer(ctx, t); err != nil {
		log.Debug("Cannot add finalizer", "error", err)
		t.SetConditions(nddv1.ReconcileError(err), nddv1.Unavailable())
		t.SetDiscoveryInfo(nil)
		return reconcile.Result{Requeue: true}, errors.Wrap(r.client.Status().Update(ctx, t), errUpdateStatus)
	}

	// stop/delete target
	r.targetCh <- targetchannel.TargetMsg{
		Target:    req.NamespacedName.String(),
		Operation: targetchannel.Start,
	}

	// The resource is up to date, we reconcile after the pollInterval to
	// regularly validate if the Target CR is still up to date
	log.Debug("Target is created", "requeue-after", time.Now().Add(r.pollInterval))
	//t.SetConditions(nddv1.ReconcileSuccess(), nddv1.Available())
	return reconcile.Result{RequeueAfter: r.pollInterval}, errors.Wrap(r.client.Status().Update(ctx, t), errUpdateStatus)
}
