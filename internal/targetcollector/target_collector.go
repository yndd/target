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

package targetcollector

import (
	"context"
	"time"

	gnmictarget "github.com/karimra/gnmic/target"
	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/pkg/errors"
	"github.com/yndd/cache/pkg/cache"
	"github.com/yndd/ndd-runtime/pkg/logging"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

const (
	// timers
	//defaultTargetReceiveBuffer = 1000
	//defaultRetryTimer          = 10 * time.Second

	// errors
	errCreateGnmiClient          = "cannot create gnmi client"
	errCreateSubscriptionRequest = "cannot create subscription request"
)

// Option can be used to manipulate Collector config.
type Option func(Collector)

// Collector defines the interfaces for the collector
type Collector interface {
	// add a logger to Collector
	WithLogger(log logging.Logger)
	// add a cache to Collector
	WithCache(c cache.Cache)
	// add a gnmiclient
	WithGNMIClient(gnmiclient *gnmictarget.Target)
	// add k8s event channels to Collector
	WithEventCh(eventChs map[string]chan event.GenericEvent)
	// start the target collector
	Start() error
	// stop the target collector
	Stop() error
}

// WithLogger specifies how the collector logs messages.
func WithLogger(log logging.Logger) Option {
	return func(d Collector) {
		d.WithLogger(log)
	}
}

// WithCache specifies the cache to use within the collector.
func WithCache(c cache.Cache) Option {
	return func(d Collector) {
		d.WithCache(c)
	}
}

// WithCache specifies the k8s event channels to use within the collector.
func WithEventCh(eventChs map[string]chan event.GenericEvent) Option {
	return func(o Collector) {
		o.WithEventCh(eventChs)
	}
}

// WithCache specifies the k8s event channels to use within the collector.
func WithGNMIClient(gnmiclient *gnmictarget.Target) Option {
	return func(o Collector) {
		o.WithGNMIClient(gnmiclient)
	}
}

// collector is the implementation of Collector interface
type collector struct {
	// config
	//namespace     string
	nsTargetName  string
	subscriptions []*Subscription
	// gnmi client
	gnmiclient *gnmictarget.Target
	// cache
	cache cache.Cache
	// k8s event channels
	eventChs map[string]chan event.GenericEvent
	// context, stop channels
	ctx    context.Context
	stopCh chan struct{} // used to stop the child go routines if the target gets deleted
	log    logging.Logger
}

// NewCollector creates a new GNMI collector
func New(ctx context.Context, nsTargetName string, paths []*string, opts ...Option) (Collector, error) {
	c := &collector{
		//namespace: namespace,
		nsTargetName: nsTargetName,
		subscriptions: []*Subscription{
			{
				name:  "target-config-collector",
				paths: paths,
			},
		},
		ctx:    ctx,
		stopCh: make(chan struct{}),
	}
	for _, opt := range opts {
		opt(c)
	}

	/*
		if tc.BufferSize == 0 {
			tc.BufferSize = defaultTargetReceiveBuffer
		}
		if tc.RetryTimer <= 0 {
			tc.RetryTimer = defaultRetryTimer
		}

		c.gnmicTarget = gnmictarget.NewTarget(tc)
		if err := c.gnmicTarget.CreateGNMIClient(ctx, grpc.WithBlock()); err != nil { // TODO add dialopts
			return nil, errors.Wrap(err, errCreateGnmiClient)
		}
	*/

	return c, nil
}

func (c *collector) WithLogger(log logging.Logger) {
	c.log = log
}

func (c *collector) WithCache(tc cache.Cache) {
	c.cache = tc
}

func (c *collector) WithGNMIClient(gnmiclient *gnmictarget.Target) {
	c.gnmiclient = gnmiclient
}

func (c *collector) WithEventCh(eventChs map[string]chan event.GenericEvent) {
	c.eventChs = eventChs
}

func (c *collector) getGNMIClient() *gnmictarget.Target {
	return c.gnmiclient
}

func (c *collector) GetSubscriptions() []*Subscription {
	return c.subscriptions
}

func (c *collector) GetSubscription(subName string) *Subscription {
	for _, s := range c.GetSubscriptions() {
		if s.GetName() == subName {
			return s
		}
	}
	return nil
}

// Start starts the target collector and the gnmi subscription
func (c *collector) Start() error {
	log := c.log.WithValues("target", c.nsTargetName)
	log.Debug("starting target config collector...")

	go func() {
		for {
			select {
			case <-c.ctx.Done():
				return
			default:
				err := c.run()
				switch err {
				case nil:
					// stop channel closed
					return
				case context.Canceled:
					// target collector stopped
					return
				default:
					log.Debug("error starting target collector", "error", err)
					time.Sleep(time.Second)
					continue
				}
			}
		}
	}()
	return nil
}

// run metric collector
func (c *collector) run() error {
	log := c.log.WithValues("target", c.nsTargetName)
	log.Debug("running target config collector...")

	c.ctx, c.subscriptions[0].cfn = context.WithCancel(c.ctx)

	// this subscription is a go routine that runs until you send a stop through the stopCh
	go c.startSubscription(c.ctx, &gnmi.Path{}, c.GetSubscriptions())

	chanSubResp, chanSubErr := c.getGNMIClient().ReadSubscriptions()

	// run the response handler
	for {
		select {
		case resp := <-chanSubResp:
			c.handleSubscription(resp.Response)
		case tErr := <-chanSubErr:
			c.log.Debug("subscribe", "error", tErr)
			return errors.New("handle subscription error")

		// stop cases
		// canceled when the subscription is stopped
		case <-c.ctx.Done(): // canceled when the subscription is stopped
			c.log.Debug("subscription stopped", "error", c.ctx.Err())
			return c.ctx.Err()
		// the whole target collector is stopped
		case <-c.stopCh: // the whole target collector is stopped
			c.stopSubscription(c.subscriptions[0])
			c.log.Debug("Stopping target collector process...")
			return nil
		}
	}
}

// StartSubscription starts a subscription
func (c *collector) startSubscription(ctx context.Context, prefix *gnmi.Path, s []*Subscription) error {
	log := c.log.WithValues("target", c.nsTargetName)
	log.Debug("subscription start...")
	// initialize new subscription

	req, err := createSubscriptionRequest(prefix, s[0])
	if err != nil {
		c.log.Debug(errCreateSubscriptionRequest, "error", err)
		return errors.Wrap(err, errCreateSubscriptionRequest)
	}

	//log.Debug("Subscription", "Request", req)
	go c.gnmiclient.Subscribe(ctx, req, s[0].GetName())
	log.Debug("subscription started ...")
	return nil
}

// StartGnmiSubscriptionHandler starts gnmi subscription
func (c *collector) Stop() error {
	log := c.log.WithValues("target", c.nsTargetName)
	log.Debug("stop Collector...")

	c.stopSubscription(c.GetSubscriptions()[0])
	close(c.stopCh)

	return nil
}

// StopSubscription stops a subscription
func (c *collector) stopSubscription(s *Subscription) error {
	log := c.log.WithValues("target", c.nsTargetName)
	log.Debug("stop subscription...")
	//s.stopCh <- true // trigger quit
	s.cfn()
	c.log.Debug("subscription stopped")
	return nil
}
