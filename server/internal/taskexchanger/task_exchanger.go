package taskexchanger

import (
	"context"
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"

	"github.com/go-logr/logr"
	v1 "github.com/llmariner/inference-manager/api/v1"
	"github.com/llmariner/inference-manager/server/internal/infprocessor"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// NewE creates a new E.
func NewE(
	infProcessor *infprocessor.P,
	k8sClient k8sclient.Client,
	gRPCPort int,
	localPodName string,
	podLabelKey string,
	podLabelValue string,
	logger logr.Logger,
) *E {
	return &E{
		infProcessor:  infProcessor,
		k8sClient:     k8sClient,
		gRPCPort:      gRPCPort,
		localPodName:  localPodName,
		podLabelKey:   podLabelKey,
		podLabelValue: podLabelValue,
		logger:        logger.WithName("taskexchanger"),
		taskReceivers: map[string]*taskReceiver{},
		taskSenders:   map[string]*taskSender{},
	}
}

// E is a task exchanger that allows multiple inference servers to exchange tasks.
//
// It has two main components: task senders and task receivers. A task
// receiver is a gRPC client that connects to other server instances.
// It behaves like a gRPC client that an engine has. It receives tasks
// from other servers and scheduels them to the inference processor so
// that they are forwared to engines.
//
// A task sender is a gRPC server that receives connections from other
// server instances. It registers engines reported by other services
// to the inference processor and sends tasks to a server if locally
// connected engines don't have a specified model.
//
// The following shows the data flow of the task exchanger:
//
// Engine registration:
//
// local server's task receiver
// --> remote server's internal gRPC server
// --> remote server's task sender
// --> remote server's infprocessor.P
//
// New task scheduling:
//
// local server's HTTP handler
// --> local server's infprocessor.P
// --> local server's task sender
// --> remote server's task receiver
// --> remote server's infprocessor.P
// --> engine
//
// Task result processing:
//
// engine
// --> remote server's infprocessor.P
// --> remote server's task receiver
// --> local server's task sender
// --> local server's infprocessor.P
// --> local server's HTTP handler
type E struct {
	infProcessor *infprocessor.P

	k8sClient k8sclient.Client

	gRPCPort int

	localPodName string

	podLabelKey   string
	podLabelValue string

	logger logr.Logger

	// taskReceivers is a map of pod names to task receivers. A
	// new receiver is created when we see a remote server
	// instance and initialize a new gRPC connection.
	taskReceivers map[string]*taskReceiver

	// taskSenders is a map of pod names to task senders. A new
	// task sender is created when we receives a new gRPC streamig
	// request.
	taskSenders map[string]*taskSender

	// mu protects taskReceivers and taskSenders.
	mu sync.Mutex
}

// SetupWithManager sets up the E with the manager.
func (e *E) SetupWithManager(mgr ctrl.Manager) error {
	e.logger = mgr.GetLogger().WithName("taskExchanger")

	filter := (predicate.NewPredicateFuncs(func(object k8sclient.Object) bool {
		// Include other server pods.
		return object.GetLabels()[e.podLabelKey] == e.podLabelValue && object.GetName() != e.localPodName
	}))
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}, builder.WithPredicates(filter)).
		WithLogConstructor(func(r *reconcile.Request) logr.Logger {
			if r != nil {
				return mgr.GetLogger().WithValues("pod", r.NamespacedName)
			}
			return mgr.GetLogger()
		}).
		Complete(e)
}

// Reconcile reconciles the pod by creating/deleting a corresponding task receiver.
func (e *E) Reconcile(
	ctx context.Context,
	req ctrl.Request,
) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx).WithValues("serverPodName", req.Name)
	ctx = ctrl.LoggerInto(ctx, log)

	var pod corev1.Pod
	if err := e.k8sClient.Get(ctx, req.NamespacedName, &pod); err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error(err, "Failed to get pod")
			return ctrl.Result{}, err
		}

		log.Info("Pod not found. Deleting task receiver")
		e.deleteTaskReceiver(ctx, req.Name)

		return ctrl.Result{}, nil
	}

	if pod.Status.PodIP == "" {
		// IP has not yet been bound.
		return ctrl.Result{}, nil
	}

	// Check if the pods is being terminated.
	if pod.DeletionTimestamp != nil {
		// Pod has been deleted.
		return ctrl.Result{}, nil
	}

	e.createTaskReceiver(ctx, &pod)

	return ctrl.Result{}, nil
}

func (e *E) createTaskReceiver(ctx context.Context, pod *corev1.Pod) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, ok := e.taskReceivers[pod.Name]; ok {
		return
	}

	log := ctrl.LoggerFrom(ctx)
	log.Info("Creating a new task receiver")

	ctx, cancel := context.WithCancel(ctx)
	r := newTaskReceiver(
		e.infProcessor,
		e.localPodName,
		fmt.Sprintf("%s:%d", pod.Status.PodIP, e.gRPCPort),
		cancel,
		log,
	)
	e.taskReceivers[pod.Name] = r

	go func() {
		err := r.run(ctx)
		if err != nil {
			// TODO(kenji): Improve the error handling.
			log.Error(err, "Failed to run the client")
		}

		e.deleteTaskReceiver(ctx, pod.Name)

		if r.shutdownStarted() {
			log.Info("Task receiver is shutdown. Do not recreate it.")
			return
		}

		// Run the reconciliation. If the pod is still ready, a task receiver
		// is created again.
		log.Info("Task receiver stopped. Trigger reconciliation")

		// Reset the logger in the context to avoid repeatedly add "serverPodName" to logger.
		ctx = ctrl.LoggerInto(ctx, e.logger)

		if _, err := e.Reconcile(ctx, ctrl.Request{
			NamespacedName: k8sclient.ObjectKey{
				Name:      pod.Name,
				Namespace: pod.Namespace,
			},
		}); err != nil {
			log.Error(err, "Failed to trigger reconciliation")
		}
	}()
}

func (e *E) deleteTaskReceiver(ctx context.Context, serverPodName string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	log := ctrl.LoggerFrom(ctx)
	log.Info("Deleting task receiver")

	if r, ok := e.taskReceivers[serverPodName]; ok {
		r.stop()
	}
	delete(e.taskReceivers, serverPodName)
}

// AddOrUpdateServerStatus adds or udpates the server status. A new task sender is created if needed.
func (e *E) AddOrUpdateServerStatus(taskSenderSrv taskSenderSrv, status *v1.ServerStatus) {
	log := e.logger.WithValues("serverPodName", status.PodName)

	log.Info("Adding or updating server status")

	e.mu.Lock()
	defer e.mu.Unlock()
	s, ok := e.taskSenders[status.PodName]
	if !ok {
		log.Info("Creating a new task sender")
		s = newTaskSender(taskSenderSrv, e.infProcessor, log)
		e.taskSenders[status.PodName] = s
	}

	s.addOrUpdateEngines(status.EngineStatuses)
}

// RemoveServer removes the server.
func (e *E) RemoveServer(serverPodName string) {
	log := e.logger.WithValues("serverPodName", serverPodName)
	log.Info("Deleting task sender")

	e.mu.Lock()
	defer e.mu.Unlock()
	s, ok := e.taskSenders[serverPodName]
	if !ok {
		return
	}

	s.removeAllEngines()

	delete(e.taskSenders, serverPodName)
}

// StartGracefulShutdown makes the task receiver send task statuses with no ready engines
// so that this server stop receiving new tasks.
func (e *E) StartGracefulShutdown() {
	e.mu.Lock()
	defer e.mu.Unlock()

	// TODO(kenji): Make local engines connect to other servers.
	// If there is an

	for _, r := range e.taskReceivers {
		r.startGracefulShutdown()
	}
}
