FROM docker:29.7@sha256:3ef33f2e220b79ed3ef3b99d81746f06f306cd6340e2cb7331d17ae996e74cb6

RUN apk add bash

COPY entrypoint.sh /entrypoint.sh

ENTRYPOINT ["/bin/bash", "-x", "/entrypoint.sh"]
