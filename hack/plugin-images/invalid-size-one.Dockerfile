FROM busybox:1.37.0@sha256:ea84d3a2fd24875f68dc79b733972acadf9ef707baaf0f2cb605ddb2be403826 AS builder

COPY myheader/schema.lua /myheader/
RUN dd if=/dev/urandom of=/myheader/handler.lua bs=1M count=2


FROM scratch

COPY --from=builder /myheader /
