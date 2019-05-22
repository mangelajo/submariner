package framework

import (
    "fmt"
    "strings"
    "time"

    "github.com/onsi/ginkgo"
    "k8s.io/apimachinery/pkg/runtime/schema"
    "k8s.io/apimachinery/pkg/util/uuid"
    "k8s.io/client-go/kubernetes/scheme"
    "k8s.io/client-go/rest"

    corev1 "k8s.io/api/core/v1"
    v1 "k8s.io/api/core/v1"
    apierrors "k8s.io/apimachinery/pkg/api/errors"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    kubeclientset "k8s.io/client-go/kubernetes"

    . "github.com/onsi/gomega"
)

const (
    // How long to try single API calls (like 'get' or 'list'). Used to prevent
    // transient failures from failing tests.
    DefaultSingleCallTimeout = 30 * time.Second
    // Polling interval while trying to create objects
    PollInterval = 100 * time.Millisecond
)

// Framework supports common operations used by e2e tests; it will keep a client & a namespace for you.
// Eventual goal is to merge this with integration test framework.
type Framework struct {
    BaseName string

    // Set together with creating the ClientSet and the namespace.
    // Guaranteed to be unique in the cluster even when running the same
    // test multiple times in parallel.
    UniqueName string

    ClientSet   *kubeclientset.Clientset

    SkipNamespaceCreation    bool            // Whether to skip creating a namespace
    Namespace                *v1.Namespace   // Every test has at least one namespace unless creation is skipped
    namespacesToDelete       []*v1.Namespace // Some tests have more than one.
    NamespaceDeletionTimeout time.Duration

    // To make sure that this framework cleans up after itself, no matter what,
    // we install a Cleanup action before each test and clear it after.  If we
    // should abort, the AfterSuite hook should run all Cleanup actions.
    cleanupHandle            CleanupActionHandle

    // configuration for framework's client
    Options Options

}

// Options is a struct for managing test framework options.
type Options struct {
    ClientQPS    float32
    ClientBurst  int
    GroupVersion *schema.GroupVersion
}

// NewDefaultFramework makes a new framework and sets up a BeforeEach/AfterEach for
// you (you can write additional before/after each functions).
func NewDefaultFramework(baseName string) *Framework {
    options := Options{
        ClientQPS:   20,
        ClientBurst: 50,
    }
    return NewFramework(baseName, options, nil)
}

// NewFramework creates a test framework.
func NewFramework(baseName string, options Options, client *kubeclientset.Clientset) *Framework {
    f := &Framework{
        BaseName:                 baseName,
        Options:                  options,
        ClientSet:                client,
    }

    ginkgo.BeforeEach(f.BeforeEach)
    ginkgo.AfterEach(f.AfterEach)

    return f
}


func (f *Framework) BeforeEach() {
    // workaround for a bug in ginkgo.
    // https://github.com/onsi/ginkgo/issues/222
    f.cleanupHandle = AddCleanupAction(f.AfterEach)

    if f.ClientSet == nil {
        ginkgo.By("Creating a kubernetes client")

        f.ClientSet = f.createKubernetesClient()

    }

    if !f.SkipNamespaceCreation {
        ginkgo.By(fmt.Sprintf("Building a namespace api object, basename %s", f.BaseName))
        namespace := f.CreateNamespace(f.BaseName, map[string]string{
            "e2e-framework": f.BaseName,
        })

        f.Namespace = namespace
        f.UniqueName = f.Namespace.GetName()
    } else {
        f.UniqueName = string(uuid.NewUUID())
    }

}

func (f *Framework) createKubernetesClient() *kubeclientset.Clientset {

    restConfig, _, err := loadConfig(TestContext.KubeConfig, TestContext.KubeContext)
    Expect(err).NotTo(HaveOccurred())

    testDesc := ginkgo.CurrentGinkgoTestDescription()
    if len(testDesc.ComponentTexts) > 0 {
        componentTexts := strings.Join(testDesc.ComponentTexts, " ")
        restConfig.UserAgent = fmt.Sprintf(
            "%v -- %v",
            rest.DefaultKubernetesUserAgent(),
            componentTexts)
    }

    restConfig.QPS = f.Options.ClientQPS
    restConfig.Burst = f.Options.ClientBurst
    if f.Options.GroupVersion != nil {
        restConfig.GroupVersion = f.Options.GroupVersion
    }
    clientSet, err := kubeclientset.NewForConfig(restConfig)
    Expect(err).NotTo(HaveOccurred())

    // create scales getter, set GroupVersion and NegotiatedSerializer to default values
    // as they are required when creating a REST client.
    if restConfig.GroupVersion == nil {
        restConfig.GroupVersion = &schema.GroupVersion{}
    }
    if restConfig.NegotiatedSerializer == nil {
        restConfig.NegotiatedSerializer = scheme.Codecs
    }
    return clientSet
}


func deleteNamespace(client kubeclientset.Interface, namespaceName string) error {

    err := client.CoreV1().Namespaces().Delete(
        namespaceName,
        &metav1.DeleteOptions{});

    return err
}

// AfterEach deletes the namespace, after reading its events.
func (f *Framework) AfterEach() {
    RemoveCleanupAction(f.cleanupHandle)

    // DeleteNamespace at the very end in defer, to avoid any
    // expectation failures preventing deleting the namespace.
    defer func() {
    nsDeletionErrors := map[string]error{}
    // Whether to delete namespace is determined by 3 factors: delete-namespace flag, delete-namespace-on-failure flag and the test result
    // if delete-namespace set to false, namespace will always be preserved.
    // if delete-namespace is true and delete-namespace-on-failure is false, namespace will be preserved if test failed.

        for _, ns := range f.namespacesToDelete {
            ginkgo.By(fmt.Sprintf("Destroying namespace %q for this suite.", ns.Name))
            if err := deleteNamespace(f.ClientSet, ns.Name); err != nil {
                if !apierrors.IsNotFound(err) {
                    nsDeletionErrors[ns.Name] = err
                } else {
                    Logf("Namespace %v was already deleted", ns.Name)
                }
            }
        }

        // Paranoia-- prevent reuse!
        f.Namespace = nil
        f.ClientSet = nil
        f.namespacesToDelete = nil

        // if we had errors deleting, report them now.
        if len(nsDeletionErrors) != 0 {
            messages := []string{}
            for namespaceKey, namespaceErr := range nsDeletionErrors {
                messages = append(messages, fmt.Sprintf("Couldn't delete ns: %q: %s (%#v)", namespaceKey, namespaceErr, namespaceErr))
            }
            Failf(strings.Join(messages, ","))
        }
    }()

}

// CreateNamespace creates a namespace for e2e testing.
func (f *Framework) CreateNamespace(baseName string, labels map[string]string) *v1.Namespace {

    ns:= createTestNamespace(f.ClientSet, baseName, labels)
    f.AddNamespacesToDelete(ns)
    return ns
}

func (f *Framework) AddNamespacesToDelete(namespaces ...*v1.Namespace) {
    for _, ns := range namespaces {
        if ns == nil {
            continue
        }
        f.namespacesToDelete = append(f.namespacesToDelete, ns)
    }
}

func createTestNamespace(client kubeclientset.Interface, baseName string, labels map[string]string) *v1.Namespace {
    ginkgo.By("Creating a namespace to execute the test in")
    namespace := createNamespace(client, baseName, labels)
    ginkgo.By(fmt.Sprintf("Created test namespace %s", namespace.Name))
    return namespace
}

func createNamespace(client kubeclientset.Interface, baseName string, labels map[string]string) *v1.Namespace {
    namespaceObj := &corev1.Namespace{
        ObjectMeta: metav1.ObjectMeta{
            GenerateName: fmt.Sprintf("e2e-tests-%v-", baseName),
            Labels: labels,
        },
    }

    namespace, err := client.CoreV1().Namespaces().Create(namespaceObj)
    Expect(err).NotTo(HaveOccurred(), "Error creating namespace %v", namespaceObj)
    return namespace
}
