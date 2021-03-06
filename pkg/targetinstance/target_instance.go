package targetinstance

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"

	gnmictarget "github.com/karimra/gnmic/target"
	"github.com/karimra/gnmic/types"
	"github.com/openconfig/ygot/ygot"
	"github.com/pkg/errors"
	"github.com/yndd/cache/pkg/cache"
	"github.com/yndd/cache/pkg/origin"
	pkgv1 "github.com/yndd/ndd-core/apis/pkg/v1"
	"github.com/yndd/ndd-runtime/pkg/logging"
	"github.com/yndd/ndd-runtime/pkg/meta"
	"github.com/yndd/ndd-runtime/pkg/resource"
	"github.com/yndd/ndd-runtime/pkg/utils"
	"github.com/yndd/nddp-system/pkg/ygotnddp"
	"github.com/yndd/registrator/registrator"
	targetv1 "github.com/yndd/target/apis/target/v1"
	"github.com/yndd/target/internal/targetcollector"
	"github.com/yndd/target/internal/targetreconciler"
	"github.com/yndd/target/pkg/target"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

const (
	// Timers
	defaultTimeout = 5 * time.Second
	// Errors
	errCreateGnmiClient             = "cannot create gnmi client"
	errTargetNotRegistered          = "the target type is not registered"
	errEmptyTargetSecretReference   = "empty target secret reference"
	errCredentialSecretDoesNotExist = "credential secret does not exist"
	errEmptyTargetAddress           = "empty target address"
	errMissingUsername              = "missing username in credentials"
	errMissingPassword              = "missing password in credentials"
)

// TargetInstanceOption can be used to manipulate TargetInstance config.
type TargetInstanceOption func(TargetInstance)

// TargetInstance defines the interfaces for the target instance
type TargetInstance interface {
	// Options

	// Methods
	GetTargetConfig() (*types.TargetConfig, error)
	// CreateGNMIClient create a gnmi client for the target
	CreateGNMIClient() error
	// Init Target initializes the vendor specific gnmi calls
	InitTarget() error
	// GetCapabilities retrieves the capabilities of the target
	//GetCapabilities() (*gnmi.CapabilityResponse, error)
	// GetRunningConfig retrieves the target running config
	//GetRunningConfig() error
	// GetInitialTargetConfig retrieves the initial target instance
	GetInitialTargetConfig() error
	// InitializeSystemConfig initializes the target system config
	InitializeSystemConfig() error
	// StartTargetReconciler starts the target reconciler per target
	StartTargetReconciler() error
	// StartTargetCollector starts the target collector per target
	StartTargetCollector() error
	// StopTargetReconciler stops the target reconciler per target
	StopTargetReconciler() error
	// StopTargetCollector stops the target collector per target
	StopTargetCollector() error
	// Regsiter
	Register()
	// DeRegister
	DeRegister()
}

type TiOptions struct {
	Logger       logging.Logger
	Namespace    string
	NsTargetName string
	TargetName   string
	Cache        cache.Cache
	Client       client.Client
	//EventChs       map[string]chan event.GenericEvent
	Registrator    registrator.Registrator
	TargetRegistry target.TargetRegistry
	VendorType     targetv1.VendorType
	//TargetModel       *model.Model
}

type targetInstance struct {
	// kubernetes
	client   client.Client // used to get the target credentials
	eventChs map[string]chan event.GenericEvent

	// tartgetRegistry implements the target/vendorType specific gnmi calls
	targetRegistry target.TargetRegistry
	vendorType     targetv1.VendorType

	// controller info
	//controllerName string
	// target info
	nsTargetName string
	targetName   string
	namespace    string
	gnmiclient   *gnmictarget.Target
	paths        []*string
	cache        cache.Cache
	target       target.Target // implements specifics for the vendor type, like srl or sros
	collector    targetcollector.Collector
	reconciler   targetreconciler.Reconciler
	// dynamic discovered data
	//discoveryInfo *targetv1.DiscoveryInfo
	initialConfig interface{}
	// registrator
	registrator registrator.Registrator
	// chan
	stopCh chan struct{} // used to stop the child go routines if the target gets deleted
	ctx    context.Context
	cfn    context.CancelFunc
	// logging
	log logging.Logger
}

func NewTargetInstance(ctx context.Context, o *TiOptions, opts ...TargetInstanceOption) TargetInstance {
	ti := &targetInstance{
		log:          o.Logger,
		nsTargetName: o.NsTargetName,
		targetName:   o.TargetName,
		namespace:    o.Namespace,
		cache:        o.Cache,
		client:       o.Client,
		//eventChs:       o.EventChs,
		registrator:    o.Registrator,
		vendorType:     o.VendorType,
		targetRegistry: o.TargetRegistry,
		paths:          []*string{utils.StringPtr("/")},
		stopCh:         make(chan struct{}),
		//newTarget:    tg,
	}

	for _, opt := range opts {
		opt(ti)
	}

	ti.ctx, ti.cfn = context.WithCancel(ctx)

	return ti
}

func (ti *targetInstance) CreateGNMIClient() error {
	targetConfig, err := ti.GetTargetConfig()
	if err != nil {
		return err
	}
	ti.gnmiclient = gnmictarget.NewTarget(targetConfig)
	if err := ti.gnmiclient.CreateGNMIClient(ti.ctx, grpc.WithBlock()); err != nil { // TODO add dialopts
		return errors.Wrap(err, errCreateGnmiClient)
	}
	return nil
}

func (ti *targetInstance) InitTarget() error {
	// initialize the target which implements the specific gnmi calls for this vendor target type
	var err error
	ti.target, err = ti.targetRegistry.Initialize(ti.vendorType)
	if err != nil {
		return err
	}
	if err := ti.target.Init(
		target.WithLogging(ti.log.WithValues("target", ti.targetName)),
		target.WithTarget(ti.gnmiclient),
	); err != nil {
		return err
	}
	return nil
}

func (ti *targetInstance) GetInitialTargetConfig() error {
	log := ti.log.WithValues("nsTargetName", ti.nsTargetName)
	// get initial config through gnmi
	var err error
	ti.initialConfig, err = ti.target.GetConfig(ti.ctx)
	if err != nil {
		return err
	}

	//log.Debug("initial config", "config", ddd.initialConfig)
	config, err := json.Marshal(ti.initialConfig)
	if err != nil {
		return err
	}

	//fmt.Println(string(config))
	configCacheNsTargetName := meta.NamespacedName(ti.nsTargetName).GetPrefixNamespacedName(origin.Config)
	ce, err := ti.cache.GetEntry(configCacheNsTargetName)
	if err != nil {
		log.Debug("Get Device data from cache", "error", err)
	}

	rootStruct, err := ce.GetModel().NewConfigStruct(config, true)
	if err != nil {
		ti.log.Debug("NewConfigStruct Device config error", "error", err)
		return err
	}
	if config != nil {
		ce.SetRunningConfig(rootStruct)
	}
	return nil
}

func (ti *targetInstance) InitializeSystemConfig() error {
	log := ti.log.WithValues("nsTargetName", ti.nsTargetName)
	systemCacheNsTargetName := meta.NamespacedName(ti.nsTargetName).GetPrefixNamespacedName(origin.System)

	nddpData := &ygotnddp.Device{
		Cache: &ygotnddp.YnddSystem_Cache{
			Update:       ygot.Bool(false),
			Exhausted:    ygot.Uint32(0),
			ExhaustedNbr: ygot.Uint64(0),
		},
	}

	nddpJson, err := ygot.EmitJSON(nddpData, &ygot.EmitJSONConfig{
		Format: ygot.RFC7951,
	})
	if err != nil {
		return err
	}

	var ce cache.CacheEntry
	if ce, err = ti.cache.GetEntry(systemCacheNsTargetName); err != nil {
		log.Debug("unable to get cache entry", "error", err)
		return err
	}
	model := ce.GetModel()
	rootStruct, err := model.NewConfigStruct([]byte(nddpJson), true)
	if err != nil {
		log.Debug("NewConfigStruct System config error", "error", err)
		return err
	}
	if []byte(nddpJson) != nil {
		ce.SetRunningConfig(rootStruct)
	}

	return nil
}

func (ti *targetInstance) StartTargetReconciler() error {
	//log := ti.log.WithValues("nsTargetName", ti.nsTargetName)
	// start per target reconciler
	var err error
	ti.reconciler, err = targetreconciler.New(ti.nsTargetName,
		targetreconciler.WithTarget(ti.target),
		targetreconciler.WithCache(ti.cache),
		targetreconciler.WithLogger(ti.log),
	)
	if err != nil {
		return errors.Wrap(err, "cannot start target reconciler")
	}
	// debug
	ti.reconciler.Start()
	return nil
}

func (ti *targetInstance) StopTargetReconciler() error {
	//log := ti.log.WithValues("nsTargetName", ti.nsTargetName)
	if ti.reconciler != nil {
		return ti.reconciler.Stop()
	}
	return nil
}

func (ti *targetInstance) StartTargetCollector() error {
	//log := ti.log.WithValues("nsTargetName", ti.nsTargetName)
	// start per target collector
	var err error
	ti.collector, err = targetcollector.New(ti.ctx, ti.nsTargetName, ti.paths,
		targetcollector.WithCache(ti.cache),
		targetcollector.WithLogger(ti.log),
		targetcollector.WithEventCh(ti.eventChs),
		targetcollector.WithGNMIClient(ti.gnmiclient),
	)
	if err != nil {
		return errors.Wrap(err, "cannot start device collector")
	}
	ti.collector.Start()
	return nil
}

func (ti *targetInstance) StopTargetCollector() error {
	//log := ti.log.WithValues("nsTargetName", ti.nsTargetName)
	if ti.collector != nil {
		return ti.collector.Stop()
	}
	return nil
}

func (ti *targetInstance) GetTargetConfig() (*types.TargetConfig, error) {
	log := ti.log.WithValues("nsTargetName", ti.nsTargetName)
	//t := ti.newTarget()
	t := &targetv1.Target{}
	if err := ti.client.Get(ti.ctx, k8stypes.NamespacedName{
		Namespace: ti.namespace,
		Name:      ti.targetName,
	}, t); err != nil {
		// There's no need to requeue if we no longer exist. Otherwise we'll be
		// requeued implicitly because we return an error.
		log.Debug("cannot get target from k8s api", "error", err)
		return nil, err
	}

	creds, err := ti.getCredentials(ti.ctx, t.Spec.Properties)
	if err != nil {
		log.Debug("Cannot get credentials", "error", err)
		return nil, err
	}

	return &types.TargetConfig{
		Name:       strings.Join([]string{ti.namespace, ti.targetName}, "/"),
		Address:    t.Spec.Properties.Config.Address,
		Username:   &creds.Username,
		Password:   &creds.Password,
		Timeout:    defaultTimeout,
		Insecure:   utils.BoolPtr(t.Spec.Properties.Config.Insecure),
		SkipVerify: utils.BoolPtr(t.Spec.Properties.Config.SkipVerify),
		TLSCA:      utils.StringPtr(""), //TODO TLS
		TLSCert:    utils.StringPtr(""), //TODO TLS
		TLSKey:     utils.StringPtr(""), //TODO TLS
		Gzip:       utils.BoolPtr(false),
	}, nil
}

// Credentials holds the information for authenticating with the Server.
type Credentials struct {
	Username string
	Password string
}

// getCredentials retrieve the Login details from the target cr spec and validates the target details.
// The target cr spec info is used to build the credentials for authentication to the target.
func (ti *targetInstance) getCredentials(ctx context.Context, prop *targetv1.TargetProperties) (creds *Credentials, err error) {
	// Retrieve the secret from Kubernetes for thistarget
	credsSecret, err := ti.getSecret(ctx, prop)
	if err != nil {
		return nil, err
	}

	// Check if address is defined on the target cr
	if prop.Config.Address == "" {
		return nil, errors.New(errEmptyTargetAddress)
	}

	creds = &Credentials{
		Username: strings.TrimSuffix(string(credsSecret.Data["username"]), "\n"),
		Password: strings.TrimSuffix(string(credsSecret.Data["password"]), "\n"),
	}

	//log.Debug("Credentials", "creds", creds)

	if creds.Username == "" {
		return nil, errors.New(errMissingUsername)
	}
	if creds.Password == "" {
		return nil, errors.New(errMissingPassword)
	}

	return creds, nil
}

// Retrieve the secret containing the credentials for authentiaction with the target.
func (ti *targetInstance) getSecret(ctx context.Context, prop *targetv1.TargetProperties) (credsSecret *corev1.Secret, err error) {
	// check if credentialName is specified
	if prop.Config.CredentialName == "" {
		return nil, errors.New(errEmptyTargetSecretReference)
	}

	// check if credential secret exists
	secretKey := k8stypes.NamespacedName{
		Name:      prop.Config.CredentialName,
		Namespace: ti.namespace,
	}
	credsSecret = &corev1.Secret{}
	if err := ti.client.Get(ctx, secretKey, credsSecret); resource.IgnoreNotFound(err) != nil {
		return nil, errors.Wrap(err, errCredentialSecretDoesNotExist)
	}
	return credsSecret, nil
}

func (ti *targetInstance) Register() {
	ti.registrator.Register(ti.ctx, &registrator.Service{
		Name:         os.Getenv("TARGET_SERVICE_NAME"),
		ID:           ti.nsTargetName,
		Tags:         pkgv1.GetTargetTag(ti.namespace, ti.targetName),
		HealthChecks: []registrator.HealthKind{registrator.HealthKindTTL},
	})
}

func (ti *targetInstance) DeRegister() {
	ti.registrator.DeRegister(ti.ctx, os.Getenv("TARGET_SERVICE_NAME"))
}
