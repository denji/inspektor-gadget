package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	corev1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/kinvolk/inspektor-gadget/pkg/factory"

)

// doesKubeconfigExist checks if the kubeconfig provided by user exists
func doesKubeconfigExist(*cobra.Command, []string) error {
	var err error
	kubeconfig := viper.GetString("kubeconfig")
	if _, err = os.Stat(kubeconfig); os.IsNotExist(err) {
		return fmt.Errorf("Kubeconfig %q not found", kubeconfig)
	}
	return err
}

func execPodSimple(client *kubernetes.Clientset, node string, podCmd string) string {
	stdout, stderr, err := execPodCapture(client, node, podCmd)
	if err != nil {
		return fmt.Sprintf("%s", err) + stdout + stderr
	} else {
		return stdout + stderr
	}
}

func execPodCapture(client *kubernetes.Clientset, node string, podCmd string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	err := execPod(client, node, podCmd, &stdout, &stderr)
	return stdout.String(), stderr.String(), err
}

func execPod(client *kubernetes.Clientset, node string, podCmd string, cmdStdout io.Writer, cmdStderr io.Writer) error {
	var listOptions = metaV1.ListOptions{
		LabelSelector: "k8s-app=gadget",
		FieldSelector: "spec.nodeName=" + node + ",status.phase=Running",
	}
	pods, err := client.CoreV1().Pods("kube-system").List(listOptions)
	if err != nil {
		return err
	}
	if len(pods.Items) == 0 {
		return errors.New("not-found")
	}
	if len(pods.Items) != 1 {
		return errors.New("too-many")
	}
	podName := pods.Items[0].Name

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	loadingRules.DefaultClientConfig = &clientcmd.DefaultClientConfig
	if viper.GetString("kubeconfig") != "" {
		loadingRules.ExplicitPath = viper.GetString("kubeconfig")
	}
	overrides := &clientcmd.ConfigOverrides{ClusterDefaults: clientcmd.ClusterDefaults}
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return err
	}
	factory.SetKubernetesDefaults(restConfig)
	restClient, err := restclient.RESTClientFor(restConfig)
	if err != nil {
		return err
	}
	req := restClient.Post().
		Resource("pods").
		Name(podName).
		Namespace("kube-system").
		SubResource("exec").
		Param("container", "gadget").
		VersionedParams(&corev1.PodExecOptions{
			Container: "gadget",
			Command:   []string{"/bin/sh", "-c", podCmd},
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

        exec, err := remotecommand.NewSPDYExecutor(restConfig, "POST", req.URL())
	if err != nil {
		return err
	}

	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: cmdStdout,
		Stderr: cmdStderr,
		Tty:    false,
	})
	return err
}

func cpPodQuick(client *kubernetes.Clientset, node string, srcPath, destPath string) string {
	var listOptions = metaV1.ListOptions{
		LabelSelector: "k8s-app=gadget",
		FieldSelector: "spec.nodeName=" + node + ",status.phase=Running",
	}
	pods, err := client.CoreV1().Pods("kube-system").List(listOptions)
	if err != nil {
		return fmt.Sprintf("%s", err)
	}
	if len(pods.Items) == 0 {
		return "not-found"
	}
	if len(pods.Items) != 1 {
		return "too-many"
	}
	podName := pods.Items[0].Name

	kubectlCmd := fmt.Sprintf("kubectl ")
	if viper.GetString("kubeconfig") != "" {
		kubectlCmd += "--kubeconfig=" + viper.GetString("kubeconfig")
	}
	kubectlCmd += fmt.Sprintf(" cp %s kube-system/%s:%s", srcPath, podName, destPath)
	fmt.Println(kubectlCmd)

	cmd := exec.Command("/bin/sh", "-c", kubectlCmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Start()
	if err != nil {
		return fmt.Sprintf("%s", err)
	}
	err = cmd.Wait()
	if err != nil {
		return fmt.Sprintf("%s", err)
	}

	return ""
}

