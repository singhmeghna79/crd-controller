package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	apiextension "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"

	//_ "k8s.io/client-go/plugin/pkg/client/auth"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	"github.com/singhmeghna79/crd-controller/pkg/apis/dummy/v1alpha1"
	crd "github.com/singhmeghna79/crd-controller/pkg/apis/dummy/v1alpha1"
	myresourceclientset "github.com/singhmeghna79/crd-controller/pkg/client/clientset/versioned"
)

func getKubernetesClient() (apiextension.Interface, myresourceclientset.Interface, kubernetes.Interface) {
	// construct the path to resolve to `~/.kube/config`
	kubeConfigPath := os.Getenv("HOME") + "/.kube/config"

	// create the config from the path
	config, err := clientcmd.BuildConfigFromFlags("", kubeConfigPath)
	if err != nil {
		log.Fatalf("getClusterConfig: %v", err)
	}

	// generate the client based off of the config
	client, err := apiextension.NewForConfig(config)
	if err != nil {
		log.Fatalf("getClusterConfig: %v", err)
	}

	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("getClusterConfig: %v", err)
	}

	myresourceClient, err := myresourceclientset.NewForConfig(config)
	if err != nil {
		log.Fatalf("getClusterConfig: %v", err)
	}

	log.Info("Successfully constructed k8s client")
	return client, myresourceClient, kubeClient
}

func main() {
	// get the Kubernetes client for connectivity
	client, myresourceClient, kubeClient := getKubernetesClient()

	// Create the CRD
	err := CreateCRD(client)
	if err != nil {
		log.Fatalf("Failed to create crd: %v", err)
	}

	// Wait for the CRD to be created before we use it.
	time.Sleep(5 * time.Second)

	FirstCrd := &crd.FirstCrd{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:   "firstcrd123",
			Labels: map[string]string{"mylabel": "crd"},
		},
		Spec: crd.FirstCrdSpec{
			Message: "echo hello",
		},
		Status: crd.FirstCrdStatus{
			Name: "created",
		},
	}
	// Create the SslConfig object we create above in the k8s cluster
	resp, err := myresourceClient.DummyV1alpha1().FirstCrds("default").Create(FirstCrd)
	if err != nil {
		fmt.Printf("error while creating object: %v\n", err)
	} else {
		fmt.Printf("object created: %v\n", resp.GetName(), resp.GetLabels())
	}

	obj, err := myresourceClient.DummyV1alpha1().FirstCrds("default").Get(FirstCrd.ObjectMeta.Name, meta_v1.GetOptions{})
	if err != nil {
		log.Infof("error while getting the object %v\n", err)
	}
	fmt.Printf("FirstCrd Objects Found: \n%+v\n", obj.GetName())

	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	crdListWatcher := cache.NewListWatchFromClient(myresourceClient.DummyV1alpha1().RESTClient(), "firstcrds", v1.NamespaceDefault, fields.Everything())

	indexer, informer := cache.NewIndexerInformer(crdListWatcher, &v1alpha1.FirstCrd{}, 0, cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				queue.Add(key)
			}
		},
		UpdateFunc: func(old interface{}, new interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(new)
			if err == nil {
				queue.Add(key)
			}
		},
		DeleteFunc: func(obj interface{}) {
			// IndexerInformer uses a delta queue, therefore for deletes we have to use this
			// key function.
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err == nil {
				queue.Add(key)
			}
		},
	}, cache.Indexers{})

	controller := NewController(queue, indexer, informer, kubeClient)

	// use a channel to synchronize the finalization for a graceful shutdown
	stopCh := make(chan struct{})
	defer close(stopCh)

	// run the controller loop to process items
	go controller.Run(1, stopCh)

	// use a channel to handle OS signals to terminate and gracefully shut
	// down processing
	sigTerm := make(chan os.Signal, 1)
	signal.Notify(sigTerm, syscall.SIGTERM)
	signal.Notify(sigTerm, syscall.SIGINT)
	<-sigTerm

}
