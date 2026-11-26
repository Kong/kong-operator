FROM docker:29.10@sha256:fbbc96e0af32cdc190aff92ba97a1281a115351a723afe21af78879cc55126b0

RUN apk add bash

COPY entrypoint.sh /entrypoint.sh

ENTRYPOINT ["/bin/bash", "-x", "/entrypoint.sh"]
