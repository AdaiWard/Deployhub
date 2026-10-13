# Security policy

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability. Use the repository Security tab to create a private GitHub Security Advisory.

Include the affected component, reproduction steps, expected impact, and any suggested mitigation. Remove production credentials and personal data from the report.

## Supported versions

DeployHub is currently an early stage project. Security fixes are applied to the latest commit on `main`.

## Deployment boundary

The current worker controls Docker through the host socket. Treat it as a privileged service, restrict dashboard access, and deploy only trusted repositories. The project is not yet designed for hostile public multi tenant workloads.
