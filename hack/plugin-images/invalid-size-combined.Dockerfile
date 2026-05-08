FROM busybox:1.37.0@sha256:ea84d3a2fd24875f68dc79b733972acadf9ef707baaf0f2cb605ddb2be403826 AS builder

RUN mkdir myheader &&\
    dd if=/dev/urandom of=/myheader/handler.lua bs=512k count=1 &&\
    dd if=/dev/urandom of=/myheader/schema.lua bs=512k count=1


FROM scratch

COPY --from=builder /myheader /
