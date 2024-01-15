FROM scratch

COPY config-server /app/
WORKDIR /app

ENTRYPOINT [ "/app/config-server" ]
