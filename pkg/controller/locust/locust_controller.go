package locust

import (
	"context"

	locustloadv1alpha1 "github.com/amila-ku/locust-operator/pkg/apis/locustload/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	lc "github.com/amila-ku/go-locust-client"
)

var log = logf.Log.WithName("controller_locust")
var locustUrl = "http://localhost:8089"

// Add creates a new Locust Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileLocust{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("locust-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource Locust
	err = c.Watch(&source.Kind{Type: &locustloadv1alpha1.Locust{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Deployments and requeue the owner Locust
	err = c.Watch(&source.Kind{Type: &appsv1.Deployment{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &locustloadv1alpha1.Locust{},
	})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileLocust implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileLocust{}

// ReconcileLocust reconciles a Locust object
type ReconcileLocust struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a Locust object and makes changes based on the state read
// and what is in the Locust.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileLocust) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling Locust")

	// Fetch the Locust instance
	instance := &locustloadv1alpha1.Locust{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// Define a new Deployment object
	// pod := newPodForCR(instance)
	deployment := r.deploymentForLocust(instance)

	// Set Locust instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, deployment, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Check if this Deployment already exists
	found := &appsv1.Deployment{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: deployment.Name, Namespace: deployment.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Deployment", "Deployment.Namespace", deployment.Namespace, "Deployment.Name", deployment.Name)
		err = r.client.Create(context.TODO(), deployment)
		if err != nil {
			return reconcile.Result{}, err
		}

		// Deployment created successfully - don't requeue
		return reconcile.Result{}, nil
	} else if err != nil {
		return reconcile.Result{}, err
	}

	// Deployment already exists - don't requeue
	reqLogger.Info("Skip reconcile: Deployment already exists", "Deployment.Namespace", found.Namespace, "Deployment.Name", found.Name)

	// Service
	service := r.serviceForLocust(instance)

	// Set Locust instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, service, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Check if this Service already exists
	foundsvc := &corev1.Service{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: service.Name, Namespace: service.Namespace}, foundsvc)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Service", "Service.Namespace", service.Namespace, "Service.Name", service.Name)
		err = r.client.Create(context.TODO(), service)
		if err != nil {
			return reconcile.Result{}, err
		}

		// Service created successfully - don't requeue
		return reconcile.Result{}, nil
	} else if err != nil {
		return reconcile.Result{}, err
	}

	// Service already exists - don't requeue
	reqLogger.Info("Skip reconcile: Service already exists", "Service.Namespace", foundsvc.Namespace, "Service.Name", foundsvc.Name)

	// Locust worker deployment, limit for maximum number of slaves set to 30 
	if instance.Spec.Slaves != 0 && instance.Spec.Slaves < 30 {
		slavedeployment := r.deploymentForLocustSlaves(instance)

		// Set Locust instance as the owner and controller
		if err := controllerutil.SetControllerReference(instance, slavedeployment, r.scheme); err != nil {
			return reconcile.Result{}, err
		}
	
		// Check if this Deployment already exists
		foundslaves := &appsv1.Deployment{}
		err = r.client.Get(context.TODO(), types.NamespacedName{Name: slavedeployment.Name, Namespace: slavedeployment.Namespace}, foundslaves)
		if err != nil && errors.IsNotFound(err) {
			reqLogger.Info("Creating a new Locust Worker Deployment", "Deployment.Namespace", slavedeployment.Namespace, "Deployment.Name", slavedeployment.Name)
			err = r.client.Create(context.TODO(), slavedeployment)
			if err != nil {
				return reconcile.Result{}, err
			}
	
			// Deployment created successfully - don't requeue
			return reconcile.Result{}, nil
		} else if err != nil {
			return reconcile.Result{}, err
		}
	
		// Deployment already exists - don't requeue
		reqLogger.Info("Skip reconcile: Locust Worker Deployment already exists", "Deployment.Namespace", foundslaves.Namespace, "Deployment.Name", foundslaves.Name)

	}

	// Start load generation
	// reqLogger.Info("Start Locust load generation", "Number of users", instance.Spec.Users, "Hatch Rate", instance.Spec.HatchRate)
	// err = controlLocust(instance)

	// failed to control locust
	// if err != nil {
	// 	reqLogger.Info("Failed to Start Locust load generation", "Number of users", instance.Spec.Users, "Hatch Rate", instance.Spec.HatchRate)
	// 	return reconcile.Result{}, err
	// }

	return reconcile.Result{}, nil
}

// deploymentForLocust returns a Locust Deployment object
func (r *ReconcileLocust) deploymentForLocust(cr *locustloadv1alpha1.Locust) *appsv1.Deployment {
	ls := labelsForLocust(cr.Name)
	replicas := int32Ptr(1)

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name,
			Namespace: cr.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: ls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: ls,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image:   cr.Spec.Image,
						Name:    cr.Name,
						Command: []string{"locust", "-f", "/tasks/main.py", "--master", "-H", cr.Spec.HostURL},
						Env: []corev1.EnvVar{
							{
								Name:       "TARGET_HOST",
								Value:      cr.Spec.HostURL,
							},
						},
						Ports: []corev1.ContainerPort{
							{
								Name:          "http",
								Protocol:      corev1.ProtocolTCP,
								ContainerPort: 8089,
							},
							{
								Name:          "worker-1",
								Protocol:      corev1.ProtocolTCP,
								ContainerPort: 5557,
							},
							{
								Name:          "worker-2",
								Protocol:      corev1.ProtocolTCP,
								ContainerPort: 5558,
							},
						},
					}},
				},
			},
		},
	}
	// Set Locust instance as the owner and controller
	controllerutil.SetControllerReference(cr, dep, r.scheme)
	return dep
}

// deploymentForLocustSlaves returns a Locust Deployment object
func (r *ReconcileLocust) deploymentForLocustSlaves(cr *locustloadv1alpha1.Locust) *appsv1.Deployment {
	ls := labelsForLocust(cr.Name + "-worker")

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name + "-worker",
			Namespace: cr.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &cr.Spec.Slaves,
			Selector: &metav1.LabelSelector{
				MatchLabels: ls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: ls,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image:   cr.Spec.Image,
						Name:    cr.Name + "-worker",
						Command: []string{"locust", "--worker", "--master-host", cr.Name + "-service", "-f", "/tasks/main.py"},
					}},
				},
			},
		},
	}
	// Set Locust instance as the owner and controller
	controllerutil.SetControllerReference(cr, dep, r.scheme)
	return dep
}

// serviceForLocust returns a Service object
func (r *ReconcileLocust) serviceForLocust(cr *locustloadv1alpha1.Locust) *corev1.Service {
	ls := labelsForLocust(cr.Name)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name + "-service",
			Namespace: cr.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: ls,
			Ports: []corev1.ServicePort{
				{
					Name:          "http",
					Protocol:      corev1.ProtocolTCP,
					Port: 8089,
				},
				{
					Name:          "worker-1",
					Protocol:      corev1.ProtocolTCP,
					Port: 5557,
				},
				{
					Name:          "worker-2",
					Protocol:      corev1.ProtocolTCP,
					Port: 5558,
				},
			},
		},
	}
	// Set Locust instance as the owner and controller
	controllerutil.SetControllerReference(cr, svc, r.scheme)
	return svc
}
// labelsForLocust returns the labels for selecting the resources
// belonging to the given Locust CR name.
func labelsForLocust(name string) map[string]string {
	return map[string]string{"app": "Locust", "Locust_cr": name}
}

// getPodNames returns the pod names of the array of pods passed in
func getPodNames(pods []corev1.Pod) []string {
	var podNames []string
	for _, pod := range pods {
		podNames = append(podNames, pod.Name)
	}
	return podNames
}

// controlles locust instance in provided url.
func controlLocust(cr *locustloadv1alpha1.Locust ) error {
	locust, err := lc.New(cr.Spec.HostURL)
	if err != nil {
		return err
	}

	lcstat, err := locust.Stats()
	if err != nil {
		return err
	}

	if lcstat.UserCount == cr.Spec.Users {
		return nil
	}

	_, err = locust.GenerateLoad(cr.Spec.Users, float64(cr.Spec.HatchRate))
	if err != nil {
		return err
	}

    return nil
}

func int32Ptr(i int32) *int32 { return &i }
