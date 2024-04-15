FROM  golang:1.18-bullseye as builder
ARG jarvan
WORKDIR /workspace
ENV GOPROXY=https://goproxy.cn,direct

# COPY go.mod go.mod
# COPY go.sum go.sum
# COPY main.go main.go
COPY ./ /workspace/

#RUN --mount=type=cache,target=/go/pkg/mod \
  #--mount=type=cache,target=/root/.cache/go-build go mod download

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=$TARGETARCH go build -trimpath -a -o validating-application-standards-admission-webhook main.go


FROM harbor-sh.pocketcity.com/poker-public/alpine:3.14
ARG TARGETARCH
COPY --from=builder /workspace/validating-application-standards-admission-webhook /validating-application-standards-admission-webhook
ENTRYPOINT ["/validating-application-standards-admission-webhook"]

# FROM alpine:latest
# RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories
# RUN echo "http://mirrors.aliyun.com/alpine/latest-stable/main" > /etc/apk/repositories && echo "http://mirrors.aliyun.com/alpine/latest-stable/community" >> /etc/apk/repositories

# RUN apk add --no-cache bash