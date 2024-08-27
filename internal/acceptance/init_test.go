package acceptance

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/databricks/databricks-sdk-go"
	"github.com/databricks/databricks-sdk-go/client"
	"github.com/databricks/databricks-sdk-go/config"
	"github.com/databricks/databricks-sdk-go/experimental/mocks"
	"github.com/databricks/databricks-sdk-go/logger"
	"github.com/databricks/terraform-provider-databricks/commands"
	"github.com/databricks/terraform-provider-databricks/common"
	dbproviderlogger "github.com/databricks/terraform-provider-databricks/logger"
	"github.com/databricks/terraform-provider-databricks/provider"
	"github.com/databricks/terraform-provider-databricks/qa"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func init() {
	rand.Seed(time.Now().UnixMicro())
	databricks.WithProduct("tf-integration-tests", common.Version())
	os.Setenv("TF_LOG", "DEBUG")
	dbproviderlogger.SetLogger()
}

func workspaceLevel(t *testing.T, steps ...LegacyStep) {
	loadWorkspaceEnv(t)
	run(t, steps)
}

func accountLevel(t *testing.T, steps ...LegacyStep) {
	loadAccountEnv(t)
	run(t, steps)
}

func unityWorkspaceLevel(t *testing.T, steps ...LegacyStep) {
	loadUcwsEnv(t)
	run(t, steps)
}

func unityAccountLevel(t *testing.T, steps ...LegacyStep) {
	loadUcacctEnv(t)
	run(t, steps)
}

// A Step in a terraform acceptance test using Plugin Framework
type Step struct {
	// Terraform HCL for resources to materialize in this test step.
	Template string

	Fixtures                []qa.HTTPFixture
	MockWorkspaceClientFunc func(*mocks.MockWorkspaceClient)
	MockAccountClientFunc   func(*mocks.MockAccountClient)

	Token  string
	Client *common.DatabricksClient

	// This function is called after the template is applied. Useful for making assertions
	// or doing cleanup.
	Check func(*terraform.State) error

	// Setup function called before the template is materialized.
	PreConfig func()

	Destroy                   bool
	ExpectNonEmptyPlan        bool
	ExpectError               *regexp.Regexp
	PlanOnly                  bool
	PreventDiskCleanup        bool
	PreventPostDestroyRefresh bool
	ImportState               bool
	ImportStateVerify         bool
	ProviderFactories         map[string]func() (*schema.Provider, error)
}

func (s Step) validateMocks() error {
	isMockConfigured := s.MockAccountClientFunc != nil || s.MockWorkspaceClientFunc != nil
	isFixtureConfigured := s.Fixtures != nil
	if isFixtureConfigured && isMockConfigured {
		return fmt.Errorf("either (MockWorkspaceClientFunc, MockAccountClientFunc) or Fixtures may be set, not both")
	}
	return nil
}

func (s Step) setupClient(t *testing.T) (*common.DatabricksClient, qa.Server, error) {
	token := "..."
	if s.Token != "" {
		token = s.Token
	}
	if s.Fixtures != nil {
		client, s, err := qa.HttpFixtureClientWithToken(t, s.Fixtures, token)
		ss := qa.Server{
			Close: s.Close,
			URL:   s.URL,
		}
		return client, ss, err
	}
	mw := mocks.NewMockWorkspaceClient(t)
	ma := mocks.NewMockAccountClient(t)
	if s.MockWorkspaceClientFunc != nil {
		s.MockWorkspaceClientFunc(mw)
	}
	if s.MockAccountClientFunc != nil {
		s.MockAccountClientFunc(ma)
	}
	c := &common.DatabricksClient{
		DatabricksClient: &client.DatabricksClient{
			Config: &config.Config{},
		},
	}
	c.SetWorkspaceClient(mw.WorkspaceClient)
	c.SetAccountClient(ma.AccountClient)
	c.Config.Credentials = qa.TestCredentialsProvider{Token: token}
	return c, qa.Server{
		Close: func() {},
		URL:   "does-not-matter",
	}, nil
}

// A step in a terraform acceptance test using SDKv2
type LegacyStep struct {
	// Terraform HCL for resources to materialize in this test step.
	Template string

	// This function is called after the template is applied. Useful for making assertions
	// or doing cleanup.
	Check func(*terraform.State) error

	// Setup function called before the template is materialized.
	PreConfig func()

	Destroy                   bool
	ExpectNonEmptyPlan        bool
	ExpectError               *regexp.Regexp
	PlanOnly                  bool
	PreventDiskCleanup        bool
	PreventPostDestroyRefresh bool
	ImportState               bool
	ImportStateVerify         bool
	ProviderFactories         map[string]func() (*schema.Provider, error)
}

func createUuid() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return "10000000-2000-3000-4000-500000000000"
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// environmentTemplate asserts existence and fills in {env.VAR} & {var.RANDOM} placeholders in template.
// For writing a unit test to intercept the errors (t.Fatalf literally ends the test in failure)
func environmentTemplate(t *testing.T, template string, otherVars ...map[string]string) string {
	vars := map[string]string{
		"RANDOM":      qa.RandomName("t"),
		"RANDOM_UUID": createUuid(),
	}
	if len(otherVars) > 1 {
		skipf(t)("cannot have more than one custom variable map")
	}
	if len(otherVars) == 1 {
		for k, v := range otherVars[0] {
			vars[k] = v
		}
	}
	// pullAll otherVars
	missing := 0
	var varType, varName, value string
	r := regexp.MustCompile(`{(env|var).([^{}]*)}`)
	for _, variableMatch := range r.FindAllStringSubmatch(template, -1) {
		value = ""
		varType = variableMatch[1]
		varName = variableMatch[2]
		switch varType {
		case "env":
			value = os.Getenv(varName)
		case "var":
			value = vars[varName]
		}
		if value == "" {
			skipf(t)("Missing %s %s variable.", varType, varName)
			missing++
			continue
		}
		template = strings.ReplaceAll(template, `{`+varType+`.`+varName+`}`, value)
	}
	if missing > 0 {
		skipf(t)("please set %d variables and restart", missing)
	}
	return commands.TrimLeadingWhitespace(template)
}

// Test wrapper over terraform testing framework. Multiple steps share the same
// terraform state context.
func run(t *testing.T, steps []LegacyStep) {
	cloudEnv := os.Getenv("CLOUD_ENV")
	if cloudEnv == "" {
		t.Skip("Acceptance tests skipped unless env 'CLOUD_ENV' is set")
	}
	t.Parallel()

	protoV6ProviderFactories := map[string]func() (tfprotov6.ProviderServer, error){
		"databricks": func() (tfprotov6.ProviderServer, error) {
			ctx := context.Background()

			providerServer, err := provider.GetProviderServer(ctx)

			if err != nil {
				return nil, err
			}

			return providerServer, nil
		},
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Skip(err.Error())
	}
	awsAttrs := ""
	if cloudEnv == "aws" {
		awsAttrs = "aws_attributes {}"
	}
	vars := map[string]string{
		"CWD":            cwd,
		"STICKY_RANDOM":  qa.RandomName("s"),
		"AWS_ATTRIBUTES": awsAttrs,
	}
	ts := []resource.TestStep{}
	ctx := context.Background()

	for i, s := range steps {
		stepConfig := ""
		if s.Template != "" {
			stepConfig = environmentTemplate(t, s.Template, vars)
		}
		stepNum := i
		thisStep := s
		stepCheck := thisStep.Check
		stepPreConfig := s.PreConfig
		var providerFactories map[string]func() (*schema.Provider, error)
		if thisStep.ProviderFactories != nil {
			providerFactories = thisStep.ProviderFactories
			// If there's step override, then unset the protoV6 factories.
			protoV6ProviderFactories = nil
		}
		ts = append(ts, resource.TestStep{
			PreConfig: func() {
				if stepConfig == "" {
					return
				}
				logger.Infof(ctx, "Test %s (%s) step %d config is:\n%s",
					t.Name(), cloudEnv, stepNum,
					commands.TrimLeadingWhitespace(stepConfig))

				if stepPreConfig != nil {
					stepPreConfig()
				}
			},
			Config:                    stepConfig,
			Destroy:                   s.Destroy,
			ExpectNonEmptyPlan:        s.ExpectNonEmptyPlan,
			PlanOnly:                  s.PlanOnly,
			PreventDiskCleanup:        s.PreventDiskCleanup,
			PreventPostDestroyRefresh: s.PreventPostDestroyRefresh,
			ImportState:               s.ImportState,
			ImportStateVerify:         s.ImportStateVerify,
			ExpectError:               s.ExpectError,
			ProviderFactories:         providerFactories,
			ProtoV6ProviderFactories:  protoV6ProviderFactories,
			Check: func(state *terraform.State) error {
				if stepCheck != nil {
					return stepCheck(state)
				}
				return nil
			},
		})
	}
	resource.Test(t, resource.TestCase{
		IsUnitTest: true,
		Steps:      ts,
		CheckDestroy: func(t *terraform.State) error {
			// TODO: generically check if all of ID's are removed.
			return nil
		},
	})
}

// Remove redundancy once verified that this works
func runWithFixtureServer(t *testing.T, f ResourceFixturePluginFramework) error {
	ts := []resource.TestStep{}
	ctx := context.Background()

	for i, s := range f.Steps {
		stepConfig := ""
		if s.Template != "" {
			stepConfig = environmentTemplate(t, s.Template, map[string]string{})
		}
		stepNum := i
		thisStep := s
		stepCheck := thisStep.Check
		stepPreConfig := s.PreConfig

		err := s.validateMocks()
		if err != nil {
			return err
		}
		client, server, err := s.setupClient(t)
		if err != nil {
			return err
		}
		s.Client = client
		defer server.Close()

		config := client.Config
		config.WithTesting()
		if f.CommandMock != nil {
			client.WithCommandMock(f.CommandMock)
		}
		if f.Azure {
			config.AzureResourceID = "/subscriptions/a/resourceGroups/b/providers/Microsoft.Databricks/workspaces/c"
		}
		if f.AzureSPN {
			config.AzureClientID = "a"
			config.AzureClientSecret = "b"
			config.AzureTenantID = "c"
		}
		if f.Gcp {
			config.GoogleServiceAccount = "sa@prj.iam.gserviceaccount.com"
		}
		if f.AccountID != "" {
			config.AccountID = f.AccountID
		}
		f.setDatabricksEnvironmentForTest(client, server.URL)

		protoV6ProviderFactories := map[string]func() (tfprotov6.ProviderServer, error){
			"databricks": func() (tfprotov6.ProviderServer, error) {
				ctx := context.Background()

				// tanmaytodo
				providerServer, err := provider.GetProviderServerWithConfiguredMockClient(ctx, s.Client)
				// providerServer, err := provider.GetProviderServer(ctx)

				if err != nil {
					return nil, err
				}

				return providerServer, nil
			},
		}

		ts = append(ts, resource.TestStep{
			PreConfig: func() {
				if stepConfig == "" {
					return
				}
				logger.Infof(ctx, "Test %s step %d config is:\n%s",
					t.Name(), stepNum,
					commands.TrimLeadingWhitespace(stepConfig))

				if stepPreConfig != nil {
					stepPreConfig()
				}
			},
			Config:                    stepConfig,
			Destroy:                   s.Destroy,
			ExpectNonEmptyPlan:        s.ExpectNonEmptyPlan,
			PlanOnly:                  s.PlanOnly,
			PreventDiskCleanup:        s.PreventDiskCleanup,
			PreventPostDestroyRefresh: s.PreventPostDestroyRefresh,
			ImportState:               s.ImportState,
			ImportStateVerify:         s.ImportStateVerify,
			ExpectError:               s.ExpectError,
			ProtoV6ProviderFactories:  protoV6ProviderFactories,
			Check: func(state *terraform.State) error {
				if stepCheck != nil {
					return stepCheck(state)
				}
				return nil
			},
		})
	}
	resource.Test(t, resource.TestCase{
		IsUnitTest: true,
		Steps:      ts,
		CheckDestroy: func(t *terraform.State) error {
			// TODO: generically check if all of ID's are removed.
			return nil
		},
	})
	return nil
}

// resourceCheck calls back a function with client and resource id
func resourceCheck(name string,
	cb func(ctx context.Context, client *common.DatabricksClient, id string) error) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return fmt.Errorf("not found: %s", name)
		}
		client, err := client.New(&config.Config{})
		if err != nil {
			panic(err)
		}
		return cb(context.Background(), &common.DatabricksClient{
			DatabricksClient: client,
		}, rs.Primary.ID)
	}
}

// resourceCheckWithState calls back a function with client and resource instance state
func resourceCheckWithState(name string,
	cb func(ctx context.Context, client *common.DatabricksClient, state *terraform.InstanceState) error) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return fmt.Errorf("not found: %s", name)
		}
		client, err := client.New(&config.Config{})
		if err != nil {
			panic(err)
		}
		return cb(context.Background(), &common.DatabricksClient{
			DatabricksClient: client,
		}, rs.Primary)
	}
}

const fullCharset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
const hexCharset = "0123456789abcdef"

// GetEnvOrSkipTest proceeds with test only with that env variable
func GetEnvOrSkipTest(t *testing.T, name string) string {
	value := os.Getenv(name)
	if value == "" {
		skipf(t)("Environment variable %s is missing", name)
	}
	return value
}

func GetEnvInt64OrSkipTest(t *testing.T, name string) int64 {
	v := GetEnvOrSkipTest(t, name)
	i, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		skipf(t)("`%s` is not int64: %s", v, err)
	}
	return i
}

// RandomEmail generates random email
func RandomEmail(prefix ...string) string {
	return fmt.Sprintf("%s@example.com", RandomName(
		append([]string{"sdk-go-"}, prefix...)...))
}

// RandomName gives random name with optional prefix. e.g. qa.RandomName("tf-")
func RandomName(prefix ...string) string {
	rand.Seed(time.Now().UnixNano())
	randLen := 12
	b := make([]byte, randLen)
	for i := range b {
		b[i] = fullCharset[rand.Intn(randLen)]
	}
	if len(prefix) > 0 {
		return fmt.Sprintf("%s%s", strings.Join(prefix, ""), b)
	}
	return string(b)
}

func RandomHex(prefix string, randLen int) string {
	rand.Seed(time.Now().UnixNano())

	b := make([]byte, randLen)
	for i := range b {
		b[i] = hexCharset[rand.Intn(randLen)%len(hexCharset)]
	}
	if len(prefix) > 0 {
		return fmt.Sprintf("%s%s", prefix, b)
	}
	return string(b)
}

func skipf(t *testing.T) func(format string, args ...any) {
	if isInDebug() {
		// VSCode "debug test" feature doesn't show dlv logs,
		// so that we fail here for maintainer productivity.
		return t.Fatalf
	}
	return t.Skipf
}

// detects if test is run from "debug test" feature in VSCode
func isInDebug() bool {
	ex, _ := os.Executable()
	return strings.HasPrefix(path.Base(ex), "__debug_bin")
}

func setDebugLogger() {
	logger.DefaultLogger = &logger.SimpleLogger{
		Level: logger.LevelDebug,
	}
}

func loadWorkspaceEnv(t *testing.T) {
	initTest(t, "workspace")
	if os.Getenv("DATABRICKS_ACCOUNT_ID") != "" {
		skipf(t)("Skipping workspace test on account level")
	}
}

func loadAccountEnv(t *testing.T) {
	initTest(t, "account")
	if os.Getenv("DATABRICKS_ACCOUNT_ID") == "" {
		skipf(t)("Skipping account test on workspace level")
	}
}

func loadUcwsEnv(t *testing.T) {
	initTest(t, "ucws")
	if os.Getenv("TEST_METASTORE_ID") == "" {
		skipf(t)("Skipping non-Unity Catalog test")
	}
	if os.Getenv("DATABRICKS_ACCOUNT_ID") != "" {
		skipf(t)("Skipping workspace test on account level")
	}
}

func loadUcacctEnv(t *testing.T) {
	initTest(t, "ucacct")
	if os.Getenv("TEST_METASTORE_ID") == "" {
		skipf(t)("Skipping non-Unity Catalog test")
	}
	if os.Getenv("DATABRICKS_ACCOUNT_ID") == "" {
		skipf(t)("Skipping account test on workspace level")
	}
}

func startServer() {

}

func loadFakeEnv(t *testing.T) {

}

func isAws(t *testing.T) bool {
	awsCloudEnvs := []string{"MWS", "aws", "ucws", "ucacct"}
	return isCloudEnvInList(t, awsCloudEnvs)
}

func isAzure(t *testing.T) bool {
	azureCloudEnvs := []string{"azure", "azure-ucacct"}
	return isCloudEnvInList(t, azureCloudEnvs)
}

func isGcp(t *testing.T) bool {
	gcpCloudEnvs := []string{"gcp-accounts", "gcp-ucacct", "gcp-ucws", "gcp"}
	return isCloudEnvInList(t, gcpCloudEnvs)
}

func isCloudEnvInList(t *testing.T, cloudEnvs []string) bool {
	cloudEnv := os.Getenv("CLOUD_ENV")
	if cloudEnv == "" {
		skipf(t)("Acceptance tests skipped unless env 'CLOUD_ENV' is set")
	}
	return slices.Contains(cloudEnvs, cloudEnv)
}

func isAuthedAsWorkspaceServicePrincipal(ctx context.Context) (bool, error) {
	w := databricks.Must(databricks.NewWorkspaceClient())
	user, err := w.CurrentUser.Me(ctx)
	if err != nil {
		return false, err
	}
	for _, emailValue := range user.Emails {
		if emailValue.Primary && strings.Contains(emailValue.Value, "@") {
			return false, nil
		}
	}
	return true, nil
}

func initTest(t *testing.T, key string) {
	setDebugLogger()
	loadDebugEnvIfRunsFromIDE(t, key)
	t.Log(GetEnvOrSkipTest(t, "CLOUD_ENV"))
}

// loads debug environment from ~/.databricks/debug-env.json
func loadDebugEnvIfRunsFromIDE(t *testing.T, key string) {
	if !isInDebug() {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("cannot find user home: %s", err)
	}
	raw, err := os.ReadFile(filepath.Join(home, ".databricks/debug-env.json"))
	if err != nil {
		t.Fatalf("cannot load ~/.databricks/debug-env.json: %s", err)
	}
	var conf map[string]map[string]string
	err = json.Unmarshal(raw, &conf)
	if err != nil {
		t.Fatalf("cannot parse ~/.databricks/debug-env.json: %s", err)
	}
	vars, ok := conf[key]
	if !ok {
		t.Fatalf("~/.databricks/debug-env.json#%s not configured", key)
	}
	for k, v := range vars {
		os.Setenv(k, v)
	}
}
