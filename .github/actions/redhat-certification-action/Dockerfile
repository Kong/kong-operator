FROM docker:29.9@sha256:ba3d67c44952ffc448696659a36ef91dd813f03a9c70a2bc0ad1d286d542f4fb

RUN apk add bash

COPY entrypoint.sh /entrypoint.sh

ENTRYPOINT ["/bin/bash", "-x", "/entrypoint.sh"]
