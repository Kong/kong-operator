FROM docker:29.6@sha256:7bb861a04bb42bda1d237fc2cb539f9823c9b666ecfbfdbd3e534ab74c8cb976

RUN apk add bash

COPY entrypoint.sh /entrypoint.sh

ENTRYPOINT ["/bin/bash", "-x", "/entrypoint.sh"]
