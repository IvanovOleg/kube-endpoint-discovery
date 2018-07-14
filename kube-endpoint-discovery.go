/*

Kubernetes service endpoint discovery

*/

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/golang/glog"
	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}

// getHostnames extracts hostnames from the endpoint subset
func getHostnames(subsets []core.EndpointSubset) []string {
	hostnames := []string{}
	for _, ss := range subsets {
		for _, dns := range ss.Addresses {
			hostnames = append(hostnames, dns.Hostname)
		}
	}
	return hostnames
}

// getFqdn constructs FQDN names for array items
func getFqdn(hostnames []string, namespaceName string, serviceName string, domainName string) []string {
	fqdns := []string{}
	for _, hostname := range hostnames {
		fqdns = append(fqdns, hostname+"."+serviceName+"."+namespaceName+"."+domainName)
	}
	return fqdns
}

// getNodeIndex allows to get a node index for services like zookeeper
func getNodeIndex(node string) string {
	re := regexp.MustCompile(`(^\w*)-(\d)`)
	index, _ := strconv.Atoi(re.FindStringSubmatch(node)[2])
	index++
	return strconv.Itoa(index)
}

// formatOutput parepares an output in the appropriate format
func formatOutput(result []string, format string) {
	switch format {
	case "zookeeper":
		for _, host := range result {
			fmt.Printf("server%s:%s:2888:3888\n", getNodeIndex(host), host)
		}
	case "elasticsearch":
		fmt.Printf("discovery.zen.ping.unicast.hosts: [%s]\n", strings.Join(result, ", "))
	default:
		fmt.Printf(strings.Join(result, ", "))
	}
}

func parseConfig() *string {
	var kubeconfig *string
	if home := homeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()
	return kubeconfig
}

func buildExternalConfig(kubeconfig *string) *rest.Config {
	// use the current context in kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err.Error())
	}
	return config
}

var err error

func main() {
	var endpoints *core.Endpoints
	var config *rest.Config
	hosts := []string{}
	namespaceName := os.Getenv("ENDPOINT_NAMESPACE_NAME")
	serviceName := os.Getenv("ENDPOINT_SERVICE_NAME")
	domainName := os.Getenv("ENDPOINT_DOMAIN_NAME")
	kubernetesServiceHost := os.Getenv("KUBERNETES_SERVICE_HOST")
	kubernetesServicePort := os.Getenv("KUBERNETES_SERVICE_PORT")
	kubeconfigPath := parseConfig()

	//check if the app is running inside the kubernetes cluster
	if (kubernetesServiceHost != "") && (kubernetesServicePort != "") {
		config, err = rest.InClusterConfig()
		if err != nil {
			panic(err.Error())
		}
	} else {
		if _, err := os.Stat(*kubeconfigPath); err == nil {
			config = buildExternalConfig(kubeconfigPath)
		}
	}

	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	//Wait for some endpoints.
	count, _ := strconv.Atoi(os.Getenv("MINIMUM_MASTER_NODES"))
	for t := time.Now(); time.Since(t) < 5*time.Minute; time.Sleep(10 * time.Second) {
		endpoints, err = clientset.Core().Endpoints(namespaceName).Get(serviceName, metav1.GetOptions{})
		if err != nil {
			continue
		}
		hosts = getFqdn(getHostnames(endpoints.Subsets), namespaceName, serviceName, domainName)
		glog.Infof("Found %s", hosts)
		if len(hosts) > 0 && len(hosts) == count {
			break
		}
	}
	glog.Infof("Endpoints = %s", hosts)
	formatOutput(hosts, serviceName)
}
