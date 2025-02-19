package operator

import (
	"time"

	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	corev1informers "k8s.io/client-go/informers/core/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	operatorv1 "github.com/openshift/api/operator/v1"
	configv1informers "github.com/openshift/client-go/config/informers/externalversions/config/v1"
	configv1listers "github.com/openshift/client-go/config/listers/config/v1"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource"
)

type ImageRegistryCertificatesController struct {
	coreClient            corev1client.CoreV1Interface
	operatorClient        v1helpers.OperatorClient
	configMapLister       corev1listers.ConfigMapNamespaceLister
	serviceLister         corev1listers.ServiceNamespaceLister
	imageConfigLister     configv1listers.ImageLister
	openshiftConfigLister corev1listers.ConfigMapNamespaceLister

	cachesToSync []cache.InformerSynced
	queue        workqueue.RateLimitingInterface
}

func NewImageRegistryCertificatesController(
	coreClient corev1client.CoreV1Interface,
	operatorClient v1helpers.OperatorClient,
	configMapInformer corev1informers.ConfigMapInformer,
	serviceInformer corev1informers.ServiceInformer,
	imageConfigInformer configv1informers.ImageInformer,
	openshiftConfigInformer corev1informers.ConfigMapInformer,
) *ImageRegistryCertificatesController {
	c := &ImageRegistryCertificatesController{
		coreClient:            coreClient,
		operatorClient:        operatorClient,
		configMapLister:       configMapInformer.Lister().ConfigMaps(defaults.ImageRegistryOperatorNamespace),
		serviceLister:         serviceInformer.Lister().Services(defaults.ImageRegistryOperatorNamespace),
		imageConfigLister:     imageConfigInformer.Lister(),
		openshiftConfigLister: openshiftConfigInformer.Lister().ConfigMaps(defaults.OpenShiftConfigNamespace),
		queue:                 workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "ImageRegistryCertificatesController"),
	}

	configMapInformer.Informer().AddEventHandler(c.eventHandler())
	c.cachesToSync = append(c.cachesToSync, configMapInformer.Informer().HasSynced)

	serviceInformer.Informer().AddEventHandler(c.eventHandler())
	c.cachesToSync = append(c.cachesToSync, serviceInformer.Informer().HasSynced)

	imageConfigInformer.Informer().AddEventHandler(c.eventHandler())
	c.cachesToSync = append(c.cachesToSync, imageConfigInformer.Informer().HasSynced)

	openshiftConfigInformer.Informer().AddEventHandler(c.eventHandler())
	c.cachesToSync = append(c.cachesToSync, openshiftConfigInformer.Informer().HasSynced)

	return c
}

func (c *ImageRegistryCertificatesController) eventHandler() cache.ResourceEventHandler {
	const workQueueKey = "instance"
	return cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { c.queue.Add(workQueueKey) },
		UpdateFunc: func(old, new interface{}) { c.queue.Add(workQueueKey) },
		DeleteFunc: func(obj interface{}) { c.queue.Add(workQueueKey) },
	}
}

func (c *ImageRegistryCertificatesController) runWorker() {
	for c.processNextWorkItem() {
	}
}

func (c *ImageRegistryCertificatesController) processNextWorkItem() bool {
	obj, shutdown := c.queue.Get()
	if shutdown {
		return false
	}
	defer c.queue.Done(obj)

	klog.V(4).Infof("get event from workqueue")
	if err := c.sync(); err != nil {
		c.queue.AddRateLimited(workqueueKey)
		klog.Errorf("ImageRegistryCertificatesController: unable to sync: %s, requeuing", err)
	} else {
		c.queue.Forget(obj)
		klog.V(4).Infof("ImageRegistryCertificatesController: event from workqueue successfully processed")
	}
	return true
}

func (c *ImageRegistryCertificatesController) sync() error {
	g := resource.NewGeneratorCAConfig(c.configMapLister, c.imageConfigLister, c.openshiftConfigLister, c.serviceLister, c.coreClient)
	err := resource.ApplyMutator(g)
	if err != nil {
		_, _, updateError := v1helpers.UpdateStatus(c.operatorClient, v1helpers.UpdateConditionFn(operatorv1.OperatorCondition{
			Type:    "ImageRegistryCertificatesControllerDegraded",
			Status:  operatorv1.ConditionTrue,
			Reason:  "Error",
			Message: err.Error(),
		}))
		return utilerrors.NewAggregate([]error{err, updateError})
	}

	_, _, err = v1helpers.UpdateStatus(c.operatorClient, v1helpers.UpdateConditionFn(operatorv1.OperatorCondition{
		Type:   "ImageRegistryCertificatesControllerDegraded",
		Status: operatorv1.ConditionFalse,
		Reason: "AsExpected",
	}))
	return err
}

func (c *ImageRegistryCertificatesController) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	klog.Infof("Starting ImageRegistryCertificatesController")
	if !cache.WaitForCacheSync(stopCh, c.cachesToSync...) {
		return
	}

	go wait.Until(c.runWorker, time.Second, stopCh)

	klog.Infof("Started ImageRegistryCertificatesController")
	<-stopCh
	klog.Infof("Shutting down ImageRegistryCertificatesController")
}
