[working-directory: "cli"]
cli-clean:
    rm -rf cli

[working-directory: "cli"]
cli-build:
    go build -o cli -ldflags "-s -w" main.go

[working-directory: "chat/server"]
server-build:
    go build -o server -ldflags "-s -w" main.go

[working-directory: "chat/client"]
client-build:
    go build -o client -ldflags "-s -w" main.go

kafka-run:
    podman run --rm -p 9092:9092 -p 9093:9093 --name kafka-broker \
        --tmpfs /var/lib/kafka/data:rw,noexec,nosuid,size=1g \
        apache/kafka:latest