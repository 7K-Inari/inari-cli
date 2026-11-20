# Deployment-time observability CLI

All deployment sources use the same server preflight and DeploymentPlan contract:

```sh
inari deploy hiroba/payments-api --cluster clu-1 --file values.yaml
inari deploy chart oci://ghcr.io/7k-hiroba/payments-api/charts/payments-api --source hiroba --version 1.4.0 --digest sha256:<64-hex> --cluster clu-1
inari deploy template hiroba/payments-service --name payments-api
```

The default is `--observability=skip`. Opt in with `--observability=provision`, an entitled `--observability-target`, provider-allowed folder/datasource/evaluation, and exact compatible pack flags when a policy default is not present. Provisioning requires an owning team for access verification. The server revalidates all grants, policies, versions, and digests on submit.

For an existing instance:

```sh
inari resources observability preflight INSTANCE
inari resources observability provision INSTANCE
inari resources observability status INSTANCE
```

Later provisioning reloads the original application pin, never `latest`. Status prints Grafana links only when returned by a ready, current target-side verification. Pending, skipped, failed, stale, expired, or revoked states do not fabricate links. Use `-o json` or `-o yaml` for the complete server-provided plan/readiness contract.

Concrete multi-target companion rendering and deployed verification remain subject to the platform renderer gate; the CLI does not bypass it.
