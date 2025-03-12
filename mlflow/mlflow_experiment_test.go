package mlflow_test

import (
	"testing"

	"github.com/databricks/terraform-provider-databricks/internal/acceptance"
)

func TestAccMLflowExperiment(t *testing.T) {
	acceptance.WorkspaceLevel(t, acceptance.Step{
		Template: `
		resource "databricks_mlflow_experiment" "e1" {
			name = "/Shared/tf-{var.RANDOM}"
			artifact_location = "dbfs:/tmp/tf-{var.RANDOM}"
			tags {
				key = "tag1-{var.STICKY_RANDOM}"
				value = "value1-{var.STICKY_RANDOM}"
			}
			tags {
				key = "tag2-{var.STICKY_RANDOM}"
				value = "value2-{var.STICKY_RANDOM}"
			}
		}
		`,
	})
}
