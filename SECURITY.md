# Security policy

## Reporting a vulnerability

Please do not report security issues through public GitHub issues.

Use GitHub's private vulnerability reporting instead: open the
[Security tab](https://github.com/glassnode/glassnode-api-go-client/security/advisories/new)
of this repository and choose **Report a vulnerability**. The report reaches the
maintainers listed in [CODEOWNERS](CODEOWNERS) only.

Include what you can of the following:

- the affected version or commit,
- a description of the issue and its impact,
- steps or a short program that reproduces it.

You will get an acknowledgement within five working days. We will keep you
informed about the fix and credit you in the release notes unless you prefer
otherwise.

## Supported versions

Security fixes are released for the latest minor version. Update to the most
recent release before reporting.

## Scope

This repository contains the Go client only. Issues in the Glassnode API itself
or in other Glassnode products should be reported to Glassnode through the
contact options at [glassnode.com](https://glassnode.com).

## What the client does to protect credentials

- The API key is sent in the `X-Api-Key` header by default, not in the URL.
- Redirects are never followed, so credentials are not forwarded to another host.
- API keys and bearer tokens are removed from error messages.
- Request paths and query parameters are validated so a metric path cannot
  change the host or inject parameters.

If you find a way around any of these, that is a vulnerability we want to hear
about.
