# Third-Party Notices

NubeRerum is MIT licensed (see `LICENSE`). It builds on the software below; each keeps its own license. The lists come from `go list -deps` for the two binaries the Docker image contains (checked 2026-10-30).

## `things-cli` (built into the image)

| Component | License | Notes |
| --- | --- | --- |
| [`arthursoares/things-cloud-sdk`](https://github.com/arthursoares/things-cloud-sdk) v0.6.0 | MIT, Copyright (c) 2017 Raphael Randschau | The image builds `things-cli` from this release plus `patches/things-cloud-sdk-arthursoares.patch`. The patch is a derivative of the SDK and is distributed under the SDK's MIT license. |
| [`google/uuid`](https://github.com/google/uuid) v1.6.0 | BSD-3-Clause, Copyright (c) 2009, 2014 Google Inc. | |

## `nubererum` (the server)

| Component | License |
| --- | --- |
| [`go-webauthn/webauthn`](https://github.com/go-webauthn/webauthn) v0.18.2 | BSD-3-Clause |
| [`go-webauthn/x`](https://github.com/go-webauthn/x) v0.3.1 | BSD-3-Clause |
| [`fxamacker/cbor`](https://github.com/fxamacker/cbor) v2.9.4 | MIT |
| [`go-viper/mapstructure`](https://github.com/go-viper/mapstructure) v2.5.0 | MIT |
| [`golang-jwt/jwt`](https://github.com/golang-jwt/jwt) v5.3.1 | MIT |
| [`google/go-tpm`](https://github.com/google/go-tpm) v0.9.8 | Apache-2.0 |
| [`google/uuid`](https://github.com/google/uuid) v1.6.0 | BSD-3-Clause |
| [`philhofer/fwd`](https://github.com/philhofer/fwd) v1.2.0 | MIT |
| [`tinylib/msgp`](https://github.com/tinylib/msgp) v1.6.4 | MIT |
| [`x448/float16`](https://github.com/x448/float16) v0.8.4 | MIT |
| [`golang.org/x/crypto`](https://pkg.go.dev/golang.org/x/crypto) v0.57.0 | BSD-3-Clause |
| [`golang.org/x/sys`](https://pkg.go.dev/golang.org/x/sys) v0.48.0 | BSD-3-Clause |
| The Go standard library and runtime | BSD-3-Clause |

## Development only (not shipped)

| Component | License |
| --- | --- |
| [`@playwright/test`](https://playwright.dev) 1.61.0 | Apache-2.0 |

## Image base

The runtime image is based on `alpine:3.22` and adds the Alpine packages `ca-certificates` (MPL-2.0 and MIT) and `tzdata` (public domain); see the Alpine package index for details.

## Trademarks

Things and Things Cloud are trademarks of Cultured Code GmbH & Co. KG. This project is not affiliated with or endorsed by Cultured Code.
