FROM busybox:1.31.1@sha256:999f1137906d82f896a70c18ed63d2797a1562cd7d4d2c1907f681b35c30459d AS builder

COPY myheader/schema.lua /myheader/
RUN dd if=/dev/urandom of=/myheader/handler.lua bs=1M count=2


FROM scratch

COPY --from=builder /myheader /
