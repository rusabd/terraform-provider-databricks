package catalog_test

import (
	"fmt"
	"testing"

	"github.com/databricks/terraform-provider-databricks/internal/acceptance"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestUcAccDataSourceStorageCredentials(t *testing.T) {
	acceptance.UnityWorkspaceLevel(t, acceptance.Step{
		Template: `
		data "databricks_storage_credentials" "this" {
		}`,
		Check: func(s *terraform.State) error {
			r, ok := s.RootModule().Resources["data.databricks_storage_credentials.this"]
			if !ok {
				return fmt.Errorf("data not found in state")
			}
			names := r.Primary.Attributes["names.#"]
			if names == "" {
				return fmt.Errorf("names are empty: %v", r.Primary.Attributes)
			}
			return nil
		},
	})
}
