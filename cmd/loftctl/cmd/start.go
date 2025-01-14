package cmd

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"

	"github.com/sirupsen/logrus"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/kubectl/pkg/util/term"

	"github.com/loft-sh/loftctl/v3/pkg/clihelper"
	"github.com/loft-sh/loftctl/v3/pkg/config"
	"github.com/loft-sh/loftctl/v3/pkg/printhelper"
	"github.com/loft-sh/loftctl/v3/pkg/upgrade"
	"github.com/mgutz/ansi"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"

	"github.com/loft-sh/loftctl/v3/cmd/loftctl/flags"
	"github.com/loft-sh/loftctl/v3/pkg/client"
	"github.com/loft-sh/log"
	"github.com/loft-sh/log/survey"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var emailRegex = regexp.MustCompile(`^[^@]+@[^\.]+\..+$`)

// StartCmd holds the cmd flags
type StartCmd struct {
	*flags.GlobalFlags

	LocalPort   string
	Host        string
	Reset       bool
	Version     string
	Context     string
	Namespace   string
	Password    string
	Values      string
	Email       string
	ReuseValues bool
	Upgrade     bool

	ChartName string
	ChartPath string
	ChartRepo string

	NoWait           bool
	NoPortForwarding bool

	// Will be filled later
	KubeClient kubernetes.Interface
	RestConfig *rest.Config
	Log        log.Logger
}

// NewStartCmd creates a new command
func NewStartCmd(globalFlags *flags.GlobalFlags) *cobra.Command {
	cmd := &StartCmd{
		GlobalFlags: globalFlags,
		Log:         log.GetInstance(),
	}

	startCmd := &cobra.Command{
		Use:   "start",
		Short: "Start a loft instance and connect via port-forwarding",
		Long: `
#######################################################
###################### loft start #####################
#######################################################
Starts a loft instance in your Kubernetes cluster and
then establishes a port-forwarding connection.

Please make sure you meet the following requirements
before running this command:

1. Current kube-context has admin access to the cluster
2. Helm v3 must be installed
3. kubectl must be installed

#######################################################
	`,
		Args: cobra.NoArgs,
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			// Check for newer version
			upgrade.PrintNewerVersionWarning()

			return cmd.Run(cobraCmd.Context())
		},
	}

	startCmd.Flags().StringVar(&cmd.Context, "context", "", "The kube context to use for installation")
	startCmd.Flags().StringVar(&cmd.Namespace, "namespace", "loft", "The namespace to install loft into")
	startCmd.Flags().StringVar(&cmd.LocalPort, "local-port", "9898", "The local port to bind to if using port-forwarding")
	startCmd.Flags().StringVar(&cmd.Host, "host", "", "Provide a hostname to enable ingress and configure its hostname")
	startCmd.Flags().StringVar(&cmd.Password, "password", "", "The password to use for the admin account. (If empty this will be the namespace UID)")
	startCmd.Flags().StringVar(&cmd.Version, "version", "", "The loft version to install")
	startCmd.Flags().StringVar(&cmd.Values, "values", "", "Path to a file for extra loft helm chart values")
	startCmd.Flags().BoolVar(&cmd.ReuseValues, "reuse-values", true, "Reuse previous Loft helm values on upgrade")
	startCmd.Flags().BoolVar(&cmd.Upgrade, "upgrade", false, "If true, Loft will try to upgrade the release")
	startCmd.Flags().StringVar(&cmd.Email, "email", "", "The email to use for the installation")
	startCmd.Flags().BoolVar(&cmd.Reset, "reset", false, "If true, an existing loft instance will be deleted before installing loft")
	startCmd.Flags().BoolVar(&cmd.NoWait, "no-wait", false, "If true, loft will not wait after installing it")
	startCmd.Flags().BoolVar(&cmd.NoPortForwarding, "no-port-forwarding", false, "If true, loft will not do port forwarding after installing it")
	startCmd.Flags().StringVar(&cmd.ChartPath, "chart-path", "", "The local chart path to deploy Loft")
	startCmd.Flags().StringVar(&cmd.ChartRepo, "chart-repo", "https://charts.loft.sh/", "The chart repo to deploy Loft")
	startCmd.Flags().StringVar(&cmd.ChartName, "chart-name", "loft", "The chart name to deploy Loft")
	return startCmd
}

// Run executes the functionality "loft start"
func (cmd *StartCmd) Run(ctx context.Context) error {
	err := cmd.prepare()
	if err != nil {
		return err
	}
	cmd.Log.WriteString(logrus.InfoLevel, "\n")

	// Uninstall already existing Loft instance
	if cmd.Reset {
		err = clihelper.UninstallLoft(cmd.KubeClient, cmd.RestConfig, cmd.Context, cmd.Namespace, cmd.Log)
		if err != nil {
			return err
		}
	}

	// Is already installed?
	isInstalled, err := clihelper.IsLoftAlreadyInstalled(cmd.KubeClient, cmd.Namespace)
	if err != nil {
		return err
	}

	// Use default password if none is set
	if cmd.Password == "" {
		defaultPassword, err := clihelper.GetLoftDefaultPassword(cmd.KubeClient, cmd.Namespace)
		if err != nil {
			return err
		}

		cmd.Password = defaultPassword
	}

	// Upgrade Loft if already installed
	if isInstalled {
		return cmd.handleAlreadyExistingInstallation(ctx)
	}

	// Install Loft
	cmd.Log.Info("Welcome to Loft!")
	cmd.Log.Info("This installer will help you configure and deploy Loft.")

	// Get email
	email := cmd.Email
	if email == "" {
		if !term.IsTerminal(os.Stdin) {
			return fmt.Errorf("please enter an email via 'loft start --email my-email@domain.com'")
		}

		email, err = cmd.Log.Question(&survey.QuestionOptions{
			Question: "Enter your email address to create the login for your admin user",
			ValidationFunc: func(emailVal string) error {
				if !emailRegex.MatchString(emailVal) {
					return fmt.Errorf("%s is not a valid email address", emailVal)
				}
				return nil
			},
		})
		if err != nil {
			return err
		}
	}

	// make sure we are ready for installing
	err = cmd.prepareInstall()
	if err != nil {
		return err
	}

	err = cmd.upgradeLoft(email)
	if err != nil {
		return err
	}

	return cmd.success(ctx)
}

func (cmd *StartCmd) prepareInstall() error {
	// delete admin user & secret
	return clihelper.UninstallLoft(cmd.KubeClient, cmd.RestConfig, cmd.Context, cmd.Namespace, log.Discard)
}

func (cmd *StartCmd) prepare() error {
	loader, err := client.NewClientFromPath(cmd.Config)
	if err != nil {
		return err
	}
	loftConfig := loader.Config()

	// first load the kube config
	kubeClientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(clientcmd.NewDefaultClientConfigLoadingRules(), &clientcmd.ConfigOverrides{})

	// load the raw config
	kubeConfig, err := kubeClientConfig.RawConfig()
	if err != nil {
		return fmt.Errorf("there is an error loading your current kube config (%w), please make sure you have access to a kubernetes cluster and the command `kubectl get namespaces` is working", err)
	}

	// we switch the context to the install config
	contextToLoad := kubeConfig.CurrentContext
	if cmd.Context != "" {
		contextToLoad = cmd.Context
	} else if loftConfig.LastInstallContext != "" && loftConfig.LastInstallContext != contextToLoad {
		contextToLoad, err = cmd.Log.Question(&survey.QuestionOptions{
			Question:     "Seems like you try to use 'loft start' with a different kubernetes context than before. Please choose which kubernetes context you want to use",
			DefaultValue: contextToLoad,
			Options:      []string{contextToLoad, loftConfig.LastInstallContext},
		})
		if err != nil {
			return err
		}
	}
	cmd.Context = contextToLoad

	loftConfig.LastInstallContext = contextToLoad
	_ = loader.Save()

	// kube client config
	kubeClientConfig = clientcmd.NewNonInteractiveClientConfig(kubeConfig, contextToLoad, &clientcmd.ConfigOverrides{}, clientcmd.NewDefaultClientConfigLoadingRules())

	// test for helm and kubectl
	_, err = exec.LookPath("helm")
	if err != nil {
		return fmt.Errorf("seems like helm is not installed. Helm is required for the installation of loft. Please visit https://helm.sh/docs/intro/install/ for install instructions")
	}

	output, err := exec.Command("helm", "version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("seems like there are issues with your helm client: \n\n%s", output)
	}

	_, err = exec.LookPath("kubectl")
	if err != nil {
		return fmt.Errorf("seems like kubectl is not installed. Kubectl is required for the installation of loft. Please visit https://kubernetes.io/docs/tasks/tools/install-kubectl/ for install instructions")
	}

	output, err = exec.Command("kubectl", "version", "--context", contextToLoad).CombinedOutput()
	if err != nil {
		return fmt.Errorf("seems like kubectl cannot connect to your Kubernetes cluster: \n\n%s", output)
	}

	cmd.RestConfig, err = kubeClientConfig.ClientConfig()
	if err != nil {
		return fmt.Errorf("there is an error loading your current kube config (%w), please make sure you have access to a kubernetes cluster and the command `kubectl get namespaces` is working", err)
	}
	cmd.KubeClient, err = kubernetes.NewForConfig(cmd.RestConfig)
	if err != nil {
		return fmt.Errorf("there is an error loading your current kube config (%w), please make sure you have access to a kubernetes cluster and the command `kubectl get namespaces` is working", err)
	}

	// Check if cluster has RBAC correctly configured
	_, err = cmd.KubeClient.RbacV1().ClusterRoles().Get(context.Background(), "cluster-admin", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("error retrieving cluster role 'cluster-admin': %w. Please make sure RBAC is correctly configured in your cluster", err)
	}

	return nil
}

func (cmd *StartCmd) handleAlreadyExistingInstallation(ctx context.Context) error {
	enableIngress := false

	// Only ask if ingress should be enabled if --upgrade flag is not provided
	if !cmd.Upgrade && term.IsTerminal(os.Stdin) {
		cmd.Log.Info("Existing Loft instance found.")

		// Check if Loft is installed in a local cluster
		isLocal := clihelper.IsLoftInstalledLocally(cmd.KubeClient, cmd.Namespace)

		// Skip question if --host flag is provided
		if cmd.Host != "" {
			enableIngress = true
		}

		if enableIngress {
			if isLocal {
				// Confirm with user if this is a local cluster
				const (
					YesOption = "Yes"
					NoOption  = "No, my cluster is running not locally (GKE, EKS, Bare Metal etc."
				)

				answer, err := cmd.Log.Question(&survey.QuestionOptions{
					Question:     "Seems like your cluster is running locally (docker desktop, minikube, kind etc.). Is that correct?",
					DefaultValue: YesOption,
					Options: []string{
						YesOption,
						NoOption,
					},
				})
				if err != nil {
					return err
				}

				isLocal = answer == YesOption
			}

			if isLocal {
				// Confirm with user if ingress should be installed in local cluster
				const (
					YesOption = "Yes, enable the ingress for Loft anyway"
					NoOption  = "No"
				)

				answer, err := cmd.Log.Question(&survey.QuestionOptions{
					Question:     "Enabling ingress is usually only useful for remote clusters. Do you still want to deploy the ingress for Loft to your local cluster?",
					DefaultValue: NoOption,
					Options: []string{
						NoOption,
						YesOption,
					},
				})
				if err != nil {
					return err
				}

				enableIngress = answer == YesOption
			}
		}

		// Check if we need to enable ingress
		if enableIngress {
			// Ask for hostname if --host flag is not provided
			if cmd.Host == "" {
				host, err := clihelper.EnterHostNameQuestion(cmd.Log)
				if err != nil {
					return err
				}

				cmd.Host = host
			} else {
				cmd.Log.Info("Will enable Loft ingress with hostname: " + cmd.Host)
			}

			if term.IsTerminal(os.Stdin) {
				err := clihelper.EnsureIngressController(cmd.KubeClient, cmd.Context, cmd.Log)
				if err != nil {
					return errors.Wrap(err, "install ingress controller")
				}
			}
		}
	}

	// Only upgrade if --upgrade flag is present or user decided to enable ingress
	if cmd.Upgrade || enableIngress {
		err := cmd.upgradeLoft("")
		if err != nil {
			return err
		}
	}

	return cmd.success(ctx)
}

func (cmd *StartCmd) upgradeLoft(email string) error {
	extraArgs := []string{}
	if email != "" {
		extraArgs = append(extraArgs, "--set", "admin.email="+email)
	}

	if cmd.Password != "" {
		extraArgs = append(extraArgs, "--set", "admin.password="+cmd.Password)
	}
	if cmd.Host != "" {
		extraArgs = append(extraArgs, "--set", "ingress.enabled=true", "--set", "ingress.host="+cmd.Host)
	}
	if cmd.Version != "" {
		extraArgs = append(extraArgs, "--version", cmd.Version)
	}

	// Do not use --reuse-values if --reset flag is provided because this should be a new install and it will cause issues with `helm template`
	if !cmd.Reset && cmd.ReuseValues {
		extraArgs = append(extraArgs, "--reuse-values")
	}

	if cmd.Values != "" {
		absValuesPath, err := filepath.Abs(cmd.Values)
		if err != nil {
			return err
		}
		extraArgs = append(extraArgs, "--values", absValuesPath)
	}

	chartName := cmd.ChartPath
	chartRepo := ""
	if chartName == "" {
		chartName = cmd.ChartName
		chartRepo = cmd.ChartRepo
	}

	err := clihelper.UpgradeLoft(chartName, chartRepo, cmd.Context, cmd.Namespace, extraArgs, cmd.Log)
	if err != nil {
		if !cmd.Reset {
			return errors.New(err.Error() + fmt.Sprintf("\n\nIf want to purge and reinstall Loft, run: %s\n", ansi.Color("loft start --reset", "green+b")))
		}

		// Try to purge Loft and retry install
		cmd.Log.Info("Trying to delete objects blocking Loft installation")

		manifests, err := clihelper.GetLoftManifests(chartName, chartRepo, cmd.Context, cmd.Namespace, extraArgs, cmd.Log)
		if err != nil {
			return err
		}

		kubectlDelete := exec.Command("kubectl", "delete", "-f", "-", "--ignore-not-found=true", "--grace-period=0", "--force")

		buffer := bytes.Buffer{}
		buffer.Write([]byte(manifests))

		kubectlDelete.Stdin = &buffer
		kubectlDelete.Stdout = os.Stdout
		kubectlDelete.Stderr = os.Stderr

		// Ignoring potential errors here
		_ = kubectlDelete.Run()

		// Retry Loft installation
		err = clihelper.UpgradeLoft(chartName, chartRepo, cmd.Context, cmd.Namespace, extraArgs, cmd.Log)
		if err != nil {
			return errors.New(err.Error() + fmt.Sprintf("\n\nLoft installation failed. Reach out to get help:\n- via Slack: %s (fastest option)\n- via Online Chat: %s\n- via Email: %s\n", ansi.Color("https://slack.loft.sh/", "green+b"), ansi.Color("https://loft.sh/", "green+b"), ansi.Color("support@loft.sh", "green+b")))
		}
	}

	return nil
}

func (cmd *StartCmd) success(ctx context.Context) error {
	if cmd.NoWait {
		return nil
	}

	// wait until Loft is ready
	loftPod, err := cmd.waitForLoft()
	if err != nil {
		return err
	}

	if cmd.NoPortForwarding {
		return nil
	}

	// check if Loft was installed locally
	isLocal := clihelper.IsLoftInstalledLocally(cmd.KubeClient, cmd.Namespace)
	if isLocal {
		// check if loft domain secret is there
		loftRouterDomain, err := cmd.pingLoftRouter(ctx, loftPod)
		if err != nil {
			cmd.Log.Errorf("Error retrieving loft router domain: %v", err)
			cmd.Log.Info("Fallback to use port-forwarding")
		} else if loftRouterDomain != "" {
			printhelper.PrintSuccessMessageLoftRouterInstall(loftRouterDomain, cmd.Password, cmd.Log)
			return nil
		}

		// start port-forwarding
		err = cmd.startPortForwarding(ctx, loftPod)
		if err != nil {
			return err
		}

		return cmd.successLocal()
	}

	// get login link
	cmd.Log.Info("Checking Loft status...")
	host, err := clihelper.GetLoftIngressHost(cmd.KubeClient, cmd.Namespace)
	if err != nil {
		return err
	}

	// check if loft is reachable
	reachable, err := clihelper.IsLoftReachable(host)
	if !reachable || err != nil {
		const (
			YesOption = "Yes"
			NoOption  = "No, please re-run the DNS check"
		)

		answer, err := cmd.Log.Question(&survey.QuestionOptions{
			Question:     "Unable to reach Loft at https://" + host + ". Do you want to start port-forwarding instead?",
			DefaultValue: YesOption,
			Options: []string{
				YesOption,
				NoOption,
			},
		})
		if err != nil {
			return err
		}

		if answer == YesOption {
			err = cmd.startPortForwarding(ctx, loftPod)
			if err != nil {
				return err
			}

			return cmd.successLocal()
		}
	}

	return cmd.successRemote(host)
}

func (cmd *StartCmd) successLocal() error {
	printhelper.PrintSuccessMessageLocalInstall(cmd.Password, cmd.LocalPort, cmd.Log)

	blockChan := make(chan bool)
	<-blockChan
	return nil
}

func (cmd *StartCmd) successRemote(host string) error {
	ready, err := clihelper.IsLoftReachable(host)
	if err != nil {
		return err
	} else if ready {
		printhelper.PrintSuccessMessageRemoteInstall(host, cmd.Password, cmd.Log)
		return nil
	}

	// Print DNS Configuration
	printhelper.PrintDNSConfiguration(host, cmd.Log)

	cmd.Log.Info("Waiting for you to configure DNS, so loft can be reached on https://" + host)
	err = wait.PollImmediate(time.Second*5, config.Timeout(), func() (bool, error) {
		return clihelper.IsLoftReachable(host)
	})
	if err != nil {
		return err
	}

	cmd.Log.Done("Loft is reachable at https://" + host)
	printhelper.PrintSuccessMessageRemoteInstall(host, cmd.Password, cmd.Log)
	return nil
}

func (cmd *StartCmd) waitForLoft() (*corev1.Pod, error) {
	// wait for loft pod to start
	cmd.Log.Info("Waiting for Loft pod to be running...")
	loftPod, err := clihelper.WaitForReadyLoftPod(cmd.KubeClient, cmd.Namespace, cmd.Log)
	cmd.Log.Donef("Loft pod successfully started")
	if err != nil {
		return nil, err
	}

	// ensure user admin secret is there
	isNewPassword, err := clihelper.EnsureAdminPassword(cmd.KubeClient, cmd.RestConfig, cmd.Password, cmd.Log)
	if err != nil {
		return nil, err
	}

	// If password is different than expected
	if isNewPassword {
		cmd.Password = ""
	}

	return loftPod, nil
}

func (cmd *StartCmd) pingLoftRouter(ctx context.Context, loftPod *corev1.Pod) (string, error) {
	loftRouterSecret, err := cmd.KubeClient.CoreV1().Secrets(loftPod.Namespace).Get(ctx, clihelper.LoftRouterDomainSecret, metav1.GetOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			return "", nil
		}

		return "", fmt.Errorf("find loft router domain secret: %w", err)
	} else if loftRouterSecret.Data == nil || len(loftRouterSecret.Data["domain"]) == 0 {
		return "", nil
	}

	// get the domain from secret
	loftRouterDomain := string(loftRouterSecret.Data["domain"])

	// wait until loft is reachable at the given url
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
	cmd.Log.Infof("Waiting until loft is reachable at https://%s", loftRouterDomain)
	err = wait.PollUntilContextTimeout(context.TODO(), time.Second*3, time.Minute*5, true, func(ctx context.Context) (bool, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+loftRouterDomain+"/version", nil)
		if err != nil {
			return false, nil
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			return false, nil
		}

		return resp.StatusCode == http.StatusOK, nil
	})
	if err != nil {
		return "", err
	}

	return loftRouterDomain, nil
}

func (cmd *StartCmd) startPortForwarding(ctx context.Context, loftPod *corev1.Pod) error {
	stopChan, err := clihelper.StartPortForwarding(ctx, cmd.RestConfig, cmd.KubeClient, loftPod, cmd.LocalPort, cmd.Log)
	if err != nil {
		return err
	}
	go cmd.restartPortForwarding(ctx, stopChan)

	// wait until loft is reachable at the given url
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
	cmd.Log.Infof("Waiting until loft is reachable at https://localhost:%s", cmd.LocalPort)
	err = wait.PollUntilContextTimeout(context.TODO(), time.Second, config.Timeout(), true, func(ctx context.Context) (bool, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://localhost:"+cmd.LocalPort+"/version", nil)
		if err != nil {
			return false, nil
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			return false, nil
		}

		return resp.StatusCode == http.StatusOK, nil
	})
	if err != nil {
		return err
	}

	return nil
}

func (cmd *StartCmd) restartPortForwarding(ctx context.Context, stopChan chan struct{}) {
	for {
		<-stopChan
		cmd.Log.Info("Restart port forwarding")

		// wait for loft pod to start
		cmd.Log.Info("Waiting until loft pod has been started...")
		loftPod, err := clihelper.WaitForReadyLoftPod(cmd.KubeClient, cmd.Namespace, cmd.Log)
		if err != nil {
			cmd.Log.Fatalf("Error waiting for ready loft pod: %v", err)
		}

		// restart port forwarding
		stopChan, err = clihelper.StartPortForwarding(ctx, cmd.RestConfig, cmd.KubeClient, loftPod, cmd.LocalPort, cmd.Log)
		if err != nil {
			cmd.Log.Fatalf("Error starting port forwarding: %v", err)
		}

		cmd.Log.Donef("Successfully restarted port forwarding")
	}
}
