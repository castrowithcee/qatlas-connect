---
description: >
  Guides a user through setting up the Google Measurement Protocol provider: creating the stream secret in Google Analytics 4, finding the measurement ID, adding both to Qatlas, naming the tool in the tools list, and what the connection test proves.
type: knowledge
edit: shared
created: 2026-11-19
updated: 2026-11-19
---

# Set up Google Measurement Protocol

This page shows how to connect Qatlas to the Measurement Protocol of one Google Analytics 4 web data stream.
Limits, checks, and the effect on measurement data are described in the
[Google Measurement Protocol provider](README.md).

## What you need

- A Google Analytics 4 property with a web data stream. App streams are not supported.
- The measurement ID of that stream, of the form `G-` followed by upper-case letters and digits. It is not a
  secret.
- A Measurement Protocol API secret of that stream.
- Qatlas keeps the secret in the system keyring or the vault, or reads it from an environment variable.
- The service is always `https://www.google-analytics.com`; there is nothing to change for it.

Sending adds events to the property and cannot be undone. Try the tool with a separate test property first.

## Create the stream secret

In Google Analytics, open your account and property, choose **Admin**, and under **Data collection and
modification** choose **Data streams**. Select your web data stream, choose **Measurement Protocol API
secrets**, and choose **Create**. Enter a nickname, choose **Create**, and copy the secret value. The measurement
ID is part of the details of the same data stream; Google does not describe it on the page of the secret. Google changes its pages from time to time; look for these
names.

The secret is private to your organization. Keep it for Qatlas only, and rotate it when it may have leaked.

## Add it to Qatlas

Store the secret on the command line. It is read from standard input, so it does not appear in the shell
history:

```sh
printf %s "$GA_STREAM_SECRET" | qconnect credential set ga-stream-secret api_secret
```

`ga-stream-secret` is the name of the credential in your configuration and `api_secret` is its only secret.
To replace a secret, run the same command again. Then add the service with its `measurement_id` option and the
connection to the configuration file. Run `qconnect config validate` to check the file. It contacts no
provider.

```yaml
services:
  analytics-events:
    provider: google.measurement-protocol
    options:
      measurement_id: G-XXXXXXXXXX

credentials:
  ga-stream-secret:
    provider: google.measurement-protocol
    type: keyring

connections:
  analytics-events:
    service: analytics-events
    credential: ga-stream-secret
    tools:
      - google.measurement-protocol.events.send
```

The provider has no tool profile, so a new connection starts without a preselected tool. The `tools` entry is
what offers `events.send`; without it the tool is invisible and cannot be called. Every call also asks for
confirmation. Leave the target empty.

## Check the connection

In `qconnect tui`, open Connections, select the connection, and press `t`. The test runs only on your machine.
`[ok]` means the measurement ID has the right form and a secret is present.

**A passing test does not prove the secret or the data stream.** Qatlas sends nothing during the test, because
Google answers every Measurement Protocol request with success and would record the event. A wrong secret or a
wrong measurement ID is therefore not found until you look at the property.

| Result | Meaning |
| --- | --- |
| `provider-error` | The `measurement_id` option is missing or malformed, or the connection has a target |
| `auth` | The secret has an unusable form |
