FROM debian:bookworm-slim

ARG HUGO_VERSION=0.163.0
ARG TARGETARCH

ADD https://github.com/gohugoio/hugo/releases/download/v${HUGO_VERSION}/hugo_extended_${HUGO_VERSION}_linux-${TARGETARCH}.tar.gz /tmp/hugo.tar.gz
RUN tar -xzf /tmp/hugo.tar.gz -C /usr/local/bin hugo && rm /tmp/hugo.tar.gz

WORKDIR /src
EXPOSE 1313
CMD ["hugo", "server", "--bind", "0.0.0.0"]
