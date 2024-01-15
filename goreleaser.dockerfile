FROM scratch

LABEL repo="https://github.com/iptecharch/config-server"

COPY config-server /app/
WORKDIR /app

ENTRYPOINT [ "/app/config-server" ]
