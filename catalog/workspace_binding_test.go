package catalog_test

import (
	"fmt"
	"testing"

	"github.com/databricks/terraform-provider-databricks/internal/acceptance"
)

func workspaceBindingTemplateWithWorkspaceId(workspaceId string) string {
	return fmt.Sprintf(`
		# The dummy workspace needs to be assigned to the metastore for this test to pass
		resource "databricks_metastore_assignment" "this" {
			metastore_id = "{env.TEST_METASTORE_ID}"
			workspace_id = {env.DUMMY_WORKSPACE_ID}
		}

		resource "databricks_catalog" "dev" {
			name           = "dev{var.RANDOM}"
			isolation_mode = "ISOLATED"
		}

		resource "databricks_catalog" "prod" {
			name           = "prod{var.RANDOM}"
			isolation_mode = "ISOLATED"
		}

		resource "databricks_storage_credential" "external" {
			name = "cred-{var.RANDOM}"
			aws_iam_role {
				role_arn = "{env.TEST_METASTORE_DATA_ACCESS_ARN}"
			}
			isolation_mode = "ISOLATION_MODE_ISOLATED"
		}

		resource "databricks_credential" "credential" {
			name = "service-cred-{var.RANDOM}"
			aws_iam_role {
				role_arn = "{env.TEST_METASTORE_DATA_ACCESS_ARN}"
			}
			purpose = "SERVICE"
			skip_validation = true
			isolation_mode = "ISOLATION_MODE_ISOLATED"
		}

		resource "databricks_external_location" "some" {
			name            = "external-{var.RANDOM}"
			url             = "s3://{env.TEST_BUCKET}/some{var.RANDOM}"
			credential_name = databricks_storage_credential.external.id
			isolation_mode  = "ISOLATION_MODE_ISOLATED"
		}

		resource "databricks_workspace_binding" "dev" {
			catalog_name = databricks_catalog.dev.name
			workspace_id = %[1]s
		}

		resource "databricks_workspace_binding" "prod" {
			securable_name = databricks_catalog.prod.name
			securable_type = "catalog"
			workspace_id   = %[1]s
			binding_type   = "BINDING_TYPE_READ_ONLY"
		}

		resource "databricks_workspace_binding" "ext" {
			securable_name = databricks_external_location.some.id
			securable_type = "external_location"
			workspace_id   = %[1]s
		}

		resource "databricks_workspace_binding" "cred" {
			securable_name = databricks_storage_credential.external.id
			securable_type = "storage_credential"
			workspace_id   = %[1]s
		}

		resource "databricks_workspace_binding" "service_cred" {
			securable_name = databricks_credential.credential.id
			securable_type = "credential"
			workspace_id   = %[1]s
		}
	`, workspaceId)
}

func TestUcAccWorkspaceBindingToOtherWorkspace(t *testing.T) {
	acceptance.UnityWorkspaceLevel(t, acceptance.Step{
		Template: workspaceBindingTemplateWithWorkspaceId("{env.DUMMY_WORKSPACE_ID}"),
	})
}
