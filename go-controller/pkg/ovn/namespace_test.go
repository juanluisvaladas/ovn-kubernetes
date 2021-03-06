package ovn

import (
	"fmt"

	"github.com/urfave/cli"

	"github.com/ovn-org/ovn-kubernetes/go-controller/pkg/config"
	ovntest "github.com/ovn-org/ovn-kubernetes/go-controller/pkg/testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

type namespace struct{}

func newNamespaceMeta(namespace string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		UID:  types.UID(namespace),
		Name: namespace,
		Labels: map[string]string{
			"name": namespace,
		},
	}
}

func newNamespace(namespace string) *v1.Namespace {
	return &v1.Namespace{
		ObjectMeta: newNamespaceMeta(namespace),
		Spec:       v1.NamespaceSpec{},
		Status:     v1.NamespaceStatus{},
	}
}

func (n namespace) baseCmds(fexec *ovntest.FakeExec, namespace v1.Namespace) {
	fexec.AddFakeCmd(&ovntest.ExpectedCmd{
		Cmd:    "ovn-nbctl --timeout=15 --data=bare --no-heading --columns=external_ids find address_set",
		Output: fmt.Sprintf("name=%s\n", namespace.Name),
	})
	fexec.AddFakeCmd(&ovntest.ExpectedCmd{
		Cmd:    "ovn-nbctl --timeout=15 --data=bare --no-heading --columns=_uuid find address_set name=" + hashedAddressSet(namespace.Name),
		Output: fmt.Sprintf("name=%s\n", namespace.Name),
	})
}

func (n namespace) addCmds(fexec *ovntest.FakeExec, namespace v1.Namespace) {
	n.baseCmds(fexec, namespace)
	fexec.AddFakeCmdsNoOutputNoError([]string{
		fmt.Sprintf("ovn-nbctl --timeout=15 clear address_set %s addresses", hashedAddressSet(namespace.Name)),
	})
}

func (n namespace) delCmds(fexec *ovntest.FakeExec, namespace v1.Namespace) {
	fexec.AddFakeCmdsNoOutputNoError([]string{
		fmt.Sprintf("ovn-nbctl --timeout=15 --if-exists destroy address_set %s", hashedAddressSet(namespace.Name)),
	})
}

func (n namespace) addCmdsWithPods(fexec *ovntest.FakeExec, tP pod, namespace v1.Namespace) {
	n.baseCmds(fexec, namespace)
	fexec.AddFakeCmdsNoOutputNoError([]string{
		fmt.Sprintf(`ovn-nbctl --timeout=15 set address_set %s addresses="%s"`, hashedAddressSet(namespace.Name), tP.podIP),
	})
}

var _ = Describe("OVN Namespace Operations", func() {
	var app *cli.App

	BeforeEach(func() {
		// Restore global default values before each testcase
		config.RestoreDefaultConfig()

		app = cli.NewApp()
		app.Name = "test"
		app.Flags = config.Flags
	})

	Context("on startup", func() {

		It("reconciles an existing namespace with pods", func() {
			app.Action = func(ctx *cli.Context) error {

				test := namespace{}
				namespaceT := *newNamespace("namespace1")
				tP := newTPod(
					"node1",
					"10.128.1.0/24",
					"10.128.1.2",
					"10.128.1.1",
					"myPod",
					"10.128.1.4",
					"11:22:33:44:55:66",
					namespaceT.Name,
				)

				tExec := ovntest.NewFakeExec()
				test.addCmdsWithPods(tExec, tP, namespaceT)

				fakeOvn := FakeOVN{}
				fakeOvn.start(ctx, tExec,
					&v1.NamespaceList{
						Items: []v1.Namespace{
							namespaceT,
						},
					},
					&v1.PodList{
						Items: []v1.Pod{
							*newPod(namespaceT.Name, tP.podName, tP.nodeName, tP.podIP),
						},
					},
				)
				fakeOvn.controller.WatchNamespaces()

				_, err := fakeOvn.fakeClient.CoreV1().Namespaces().Get(namespaceT.Name, metav1.GetOptions{})
				Expect(err).NotTo(HaveOccurred())
				Expect(tExec.CalledMatchesExpected()).To(BeTrue())

				return nil
			}

			err := app.Run([]string{app.Name})
			Expect(err).NotTo(HaveOccurred())
		})

		It("reconciles an existing namespace without pods", func() {
			app.Action = func(ctx *cli.Context) error {

				test := namespace{}
				namespaceT := *newNamespace("namespace1")

				tExec := ovntest.NewFakeExec()
				test.addCmds(tExec, namespaceT)

				fakeOvn := FakeOVN{}
				fakeOvn.start(ctx, tExec, &v1.NamespaceList{
					Items: []v1.Namespace{
						namespaceT,
					},
				})
				fakeOvn.controller.WatchNamespaces()

				_, err := fakeOvn.fakeClient.CoreV1().Namespaces().Get(namespaceT.Name, metav1.GetOptions{})
				Expect(err).NotTo(HaveOccurred())
				Expect(tExec.CalledMatchesExpected()).To(BeTrue())

				return nil
			}

			err := app.Run([]string{app.Name})
			Expect(err).NotTo(HaveOccurred())
		})

	})

	Context("during execution", func() {

		It("reconciles a deleted namespace without pods", func() {
			app.Action = func(ctx *cli.Context) error {

				test := namespace{}
				namespaceT := *newNamespace("namespace1")

				fExec := ovntest.NewFakeExec()
				test.addCmds(fExec, namespaceT)

				fakeOvn := FakeOVN{}
				fakeOvn.start(ctx, fExec, &v1.NamespaceList{
					Items: []v1.Namespace{
						namespaceT,
					},
				})
				fakeOvn.controller.WatchNamespaces()

				namespace, err := fakeOvn.fakeClient.CoreV1().Namespaces().Get(namespaceT.Name, metav1.GetOptions{})
				Expect(err).NotTo(HaveOccurred())
				Expect(namespace).NotTo(BeNil())
				Eventually(fExec.CalledMatchesExpected).Should(BeTrue())

				test.delCmds(fExec, namespaceT)

				err = fakeOvn.fakeClient.CoreV1().Namespaces().Delete(namespaceT.Name, metav1.NewDeleteOptions(1))
				Expect(err).NotTo(HaveOccurred())
				Eventually(fExec.CalledMatchesExpected).Should(BeTrue())

				return nil
			}

			err := app.Run([]string{app.Name})
			Expect(err).NotTo(HaveOccurred())
		})

	})
})
