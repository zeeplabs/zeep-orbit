## v1.12.0-beta.2

> **Pre-release (beta).** It is published only under its own version: `latest`, `1.12` and the GitHub "Latest release" stay on v1.11.1, so nothing upgrades to it by accident. Use it in a controlled environment and report what you find; the stable v1.12.0 follows after real use.
>
> **Install it by version, and set the image tag explicitly.** The chart's default `image.tag` is `latest`, which is still v1.11.1:
>
> ```bash
> docker pull ghcr.io/zeeplabs/zeep-orbit:1.12.0-beta.2
> helm upgrade --install zeep-orbit <chart-1.12.0-beta.2.tgz from this release> --set image.tag=1.12.0-beta.2
> ```
>
> The builder and registry-proxy images use the chart `appVersion` by default (`1.12.0-beta.2`). The beta chart is attached to this release and is not in the Helm repository, so a plain `helm install` never picks it up.
>
> The dashboard of a beta install shows "New version available: v1.11.1", because the update banner only compares for equality. Ignore it.

Second beta of v1.12.0 (the **AI Frontend Builder**). It has the same scope and upgrade notes as [v1.12.0-beta.1](https://github.com/zeeplabs/zeep-orbit/releases/tag/v1.12.0-beta.1) plus fixes to the builder's agent loop. **The builder is still off by default.** An installation that does not set `BUILDER_URL` behaves as in v1.11.0.

### Fixed

- **A reply cut off by the output limit no longer burns steps.** When the model hit its output limit in the middle of a tool call, the half-written call came back as a generic "invalid arguments" error and the model tended to resend the same oversized call until the step limit. The agent is now told its call was cut off and to write large files in smaller pieces.
- **A turn stopped by a limit now ends with a handover.** When `max_steps` or `max_fix_iterations` stops a turn, the agent makes one extra model call without tools and says what is done, what is missing (or which build error persists and what was tried) and what to check next. The extra call is outside the limit and billed like any other; the turn still ends as limit reached, with its commit.
- **Provider retries are spread with jitter.** The wait before a retry (429, 503, 529) is between 0.5x and 1.5x of the backoff, so turns that hit the same rate limit do not retry together. A `Retry-After` from the provider is still honored exactly. Which failures are retried is unchanged.

### Upgrade notes

- No configuration, schema or deployment change since beta.1. Behavior changes only with the builder on.
