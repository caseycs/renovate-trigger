# Changelog

## [0.2.2](https://github.com/caseycs/renovate-trigger/compare/v0.2.1...v0.2.2) (2026-07-24)


### Miscellaneous Chores

* add dependabot for dockerfile ([da9b310](https://github.com/caseycs/renovate-trigger/commit/da9b310edb51367a41793f7ab244dc01bfb081ff))
* add dependabot for go modules ([14b0a19](https://github.com/caseycs/renovate-trigger/commit/14b0a19a96d85b91f8241f97be355250312a21e9))
* bump golang from 1.25-alpine to 1.26-alpine ([#22](https://github.com/caseycs/renovate-trigger/issues/22)) ([9ef639b](https://github.com/caseycs/renovate-trigger/commit/9ef639b7c5ccb2552d2dad0556336b4b1c0a985f))
* bump the go-dependencies group across 1 directory with 2 updates ([#23](https://github.com/caseycs/renovate-trigger/issues/23)) ([4386f1a](https://github.com/caseycs/renovate-trigger/commit/4386f1a76491cb36a848e910b504934c6c02327a))
* treat chore commits as releasable in release-please ([dc83cd2](https://github.com/caseycs/renovate-trigger/commit/dc83cd27bb635a805a5bb749a193ce182d254655))

## [0.2.1](https://github.com/caseycs/renovate-trigger/compare/v0.2.0...v0.2.1) (2026-07-24)


### Bug Fixes

* **chart:** pin numeric runAsUser/runAsGroup so runAsNonRoot passes ([#18](https://github.com/caseycs/renovate-trigger/issues/18)) ([7fae7f3](https://github.com/caseycs/renovate-trigger/commit/7fae7f3136c4332834a2ed75436e281db752029c))

## [0.2.0](https://github.com/caseycs/renovate-trigger/compare/v0.1.0...v0.2.0) (2026-07-18)


### Features

* **chart:** existing-Secret credentials, optional Ingress/IngressRoute, values-file install ([#12](https://github.com/caseycs/renovate-trigger/issues/12)) ([9c04baa](https://github.com/caseycs/renovate-trigger/commit/9c04baa68f97ff20aaf58767110a2362a89be43b))
* flush resolves dependents into a Renovate run ([#5](https://github.com/caseycs/renovate-trigger/issues/5)) ([744187f](https://github.com/caseycs/renovate-trigger/commit/744187f04f5f66d5ca3d045447d3b53523a868df))
* GitHub App client reads trigger declarations ([#3](https://github.com/caseycs/renovate-trigger/issues/3)) ([0cca023](https://github.com/caseycs/renovate-trigger/commit/0cca02338b3105436caa24d64ebb23783e2c9690))
* helm chart for the redesigned service ([#8](https://github.com/caseycs/renovate-trigger/issues/8)) ([9f97c81](https://github.com/caseycs/renovate-trigger/commit/9f97c81ef9ddcfce51010b87c81881076bb7a668))
* mutual exclusion — gate and postpone the flush ([#7](https://github.com/caseycs/renovate-trigger/issues/7)) ([1bb8cba](https://github.com/caseycs/renovate-trigger/commit/1bb8cba547c84058d1a147fc64b695268b900f4a))
* resolver expands dependencies to deduplicated dependents ([#4](https://github.com/caseycs/renovate-trigger/issues/4)) ([107aa0c](https://github.com/caseycs/renovate-trigger/commit/107aa0ce869440f25bf7a517d79647d489ebcf98))
* RunGate detects an active Renovate run ([#6](https://github.com/caseycs/renovate-trigger/issues/6)) ([511cc43](https://github.com/caseycs/renovate-trigger/commit/511cc43e6ab6faa6eef9083125f3a28bc0f17a77))


### Bug Fixes

* **ci:** author release PR via GitHub App token so CI runs without approval ([416e2c9](https://github.com/caseycs/renovate-trigger/commit/416e2c936056abf3e613bac151b6af15f38f44eb))
* **ci:** build with golang:1.25 to match go.mod (unbreak image build) ([d5f5501](https://github.com/caseycs/renovate-trigger/commit/d5f550146dc542726e6ef034cd217dac6e19ea7a))
* track the cmd entry point (anchor binary gitignore to root) ([186d0c2](https://github.com/caseycs/renovate-trigger/commit/186d0c268f20502ff6dd92030baed377ba033227))
