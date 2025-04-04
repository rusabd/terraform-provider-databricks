# NEXT CHANGELOG

## Release v1.71.0

### New Features and Improvements

 * Mark GKE-related fields for `databricks_mws_workspaces` and `databricks_mws_networks` as deprecated([#4531](https://github.com/databricks/terraform-provider-databricks/pull/4531)).

### Bug Fixes

 * Recreate `databricks_access_control_rule_set` when the `name` changes ([#4572](https://github.com/databricks/terraform-provider-databricks/pull/4572)).

### Documentation

 * Improve documentation for `databricks_access_control_rule_set` ([#4580](https://github.com/databricks/terraform-provider-databricks/pull/4580)).
 * Correct `first_on_demand` documentation for `aws_attributes` in `databricks_cluster`.
 * Added file events permissions to GCP external location documentation. ([#4415](https://github.com/databricks/terraform-provider-databricks/pull/4415)).

### Exporter

 * Add support for special selectors in `-listing` and `-services` [#4573](https://github.com/databricks/terraform-provider-databricks/pull/4573)
 * Allow the selective export of `databricks_mws_permission_assignment`, and change its service name to `idfed` instead of `access` ([#4571](https://github.com/databricks/terraform-provider-databricks/pull/4571))

### Internal Changes

* Use caching for group membership and user information retrieval to improve performance ([#4581](https://github.com/databricks/terraform-provider-databricks/pull/4581)).
