# Copyright 2024 Nokia
# Licensed under the Apache License 2.0
# SPDX-License-Identifier: Apache-2.0

FROM alpine:latest

COPY config-server /app/
WORKDIR /app

ENTRYPOINT [ "/app/config-server" ]
