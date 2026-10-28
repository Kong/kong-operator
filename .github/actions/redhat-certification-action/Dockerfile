FROM docker:29.9@sha256:9e79b879b177fbd72c34376a3dd3d91747da5db49e1f560f76aea59561471977

RUN apk add bash

COPY entrypoint.sh /entrypoint.sh

ENTRYPOINT ["/bin/bash", "-x", "/entrypoint.sh"]
