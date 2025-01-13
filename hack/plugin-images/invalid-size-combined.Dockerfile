FROM busybox:1.31.1@sha256:999f1137906d82f896a70c18ed63d2797a1562cd7d4d2c1907f681b35c30459d AS builder

RUN mkdir myheader &&\
    dd if=/dev/urandom of=/myheader/handler.lua bs=512k count=1 &&\
    dd if=/dev/urandom of=/myheader/schema.lua bs=512k count=1


FROM scratch

COPY --from=builder /myheader /
