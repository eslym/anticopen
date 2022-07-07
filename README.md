# AntiCopen
Anti copenheimer, a very simple firewall to avoid direct IP scan on Minecraft server, require a domain to work.

## Usage
```shell
# Reject handshake which not trying to connect minecraft.1ppl.me
./anticopen -bind 0.0.0.0:25565 -upstream 127.0.0.1:25575 -host minecraft.1ppl.me -proxy
```

## Build
```shell
go build
```
