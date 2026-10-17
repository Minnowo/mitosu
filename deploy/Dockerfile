# Example image for `mitosu serve`. Build from the repository root:
#   docker build -f deploy/Dockerfile -t mitosu .
# Run (config with listen = 0.0.0.0:7878; ~/.ssh read-only for keys,
# config and known_hosts; history in a volume):
#   docker run -d --name mitosu -p 127.0.0.1:7878:7878 \
#     -v ~/.ssh:/home/mitosu/.ssh:ro \
#     -v /srv/mitosu/mitosu.conf:/config/mitosu.conf:ro \
#     -v mitosu-data:/data mitosu
# Set the UI password once with:
#   docker run --rm -i -v /srv/mitosu/mitosu.conf:/config/mitosu.conf:ro \
#     -v mitosu-data:/data mitosu passwd --config /config/mitosu.conf --db /data/mitosu.db --stdin
# (or set login = off in the config if the port is only reachable by you).
# Keys must work without a passphrase prompt; an ssh-agent socket can be
# mounted too (-v "$SSH_AUTH_SOCK:/agent" -e SSH_AUTH_SOCK=/agent).
# Base images pinned by digest on 2026-10-17.

FROM node@sha256:0d1e33586362bb9e608c4e751e7a9e7edb55c9ac11461ed5666af696cc461dd4 AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund --ignore-scripts
COPY web/ ./
RUN npm run build

FROM golang@sha256:47667934874913a449b96faa7ef4777fa285d9ec29126ee80ed61c45b7cccf0e AS go
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist web/dist
RUN CGO_ENABLED=0 go build -trimpath -o /mitosu .

FROM debian@sha256:a29215f6a35e51e22adffa17f89e9d2ef06214e64a2bad10d765c46aea49f11f
RUN useradd --create-home --uid 1000 mitosu && install -d -o mitosu -g mitosu -m 0700 /data
COPY --from=go /mitosu /usr/local/bin/mitosu
USER mitosu
ENV HOME=/home/mitosu
VOLUME /data
EXPOSE 7878
ENTRYPOINT ["mitosu"]
CMD ["serve", "--config", "/config/mitosu.conf", "--db", "/data/mitosu.db"]
