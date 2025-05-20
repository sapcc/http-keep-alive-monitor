<!--
SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

http-keep-alive-monitor
===========================
This repository contains a kubernetes aware monitor that measures http keep-alive idle timeout settings of ingress backend services.
The measurements are exported as prometheus metrics.

This monitor can be used to ensure backends have a http keep-alive idle timeout configured that is as least as high as the ingress reverse proxy uses for the upstream connections.


