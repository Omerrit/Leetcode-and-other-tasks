FROM buildpack-deps:buster-scm

ENV GOLANG_VERSION=1.14.1 GOPATH=/home/executor/go GOROOT=/usr/local/go USER_ID=${USER_ID:-1000} GROUP_ID=${GROUP_ID:-1000}
ENV PATH=$GOPATH/bin:/usr/local/go/bin:$PATH
COPY ["gerrit-share.lan.crt", "server.crt", "/usr/local/share/ca-certificates/"]
# gcc for cgo
RUN apt-get update && apt-get install -y --no-install-recommends \
		g++ \
		gcc \
		libc6-dev \
		make \
        ca-certificates\
		pkg-config \
#	&& rm -rf /var/lib/apt/lists/* \
    && rm -rf /var/lib/apt/lists/*; \
    groupadd --gid ${GROUP_ID} executor && useradd -u ${USER_ID} -g executor -m executor \
    && mkdir /home/executor/go \
    && chown -R executor:executor /home/executor/ \
    && chmod -R 750 /home/executor/ \
    && update-ca-certificates ;
#ENV PATH $GOPATH/bin:/usr/local/go/bin:$PATH
#ENV GOLANG_VERSION=1.14.1 GOPATH=/go 
#ENV GOLANG_VERSION=1.14.1 GOPATH=/home/executor/go
#ENV PATH=$GOPATH/bin:/usr/local/go/bin:$PATH

RUN set -eux; \
	\
# this "case" statement is generated via "update.sh"
	dpkgArch="$(dpkg --print-architecture)"; \
	case "${dpkgArch##*-}" in \
		amd64) goRelArch='linux-amd64'; goRelSha256='2f49eb17ce8b48c680cdb166ffd7389702c0dec6effa090c324804a5cac8a7f8' ;; \
		armhf) goRelArch='linux-armv6l'; goRelSha256='04f10e345dae0d7c6c32ffd6356b47f2d4d0e8a0cb757f4ef48ead6c5bef206f' ;; \
		arm64) goRelArch='linux-arm64'; goRelSha256='5d8f2c202f35481617e24e63cca30c6afb1ec2585006c4a6ecf16c5f4928ab3c' ;; \
		i386) goRelArch='linux-386'; goRelSha256='92d465accdebbe2d0749b2f90c22ecb1fd2492435144923f88ce410cd56b6546' ;; \
		ppc64el) goRelArch='linux-ppc64le'; goRelSha256='6559201d452ee2782dfd684d59c05e3ecf789dc40a7ec0ad9ae2dd9f489c0fe1' ;; \
		s390x) goRelArch='linux-s390x'; goRelSha256='af009bd6e7729c441fec78af427743fefbf11f919c562e01b37836d835f74226' ;; \
		*) goRelArch='src'; goRelSha256='2ad2572115b0d1b4cb4c138e6b3a31cee6294cb48af75ee86bec3dca04507676'; \
			echo >&2; echo >&2 "warning: current architecture ($dpkgArch) does not have a corresponding Go binary release; will be building from source"; echo >&2 ;; \
	esac; \
	\
	url="https://golang.org/dl/go${GOLANG_VERSION}.${goRelArch}.tar.gz"; \
	wget -O go.tgz "$url"; \
	echo "${goRelSha256} *go.tgz" | sha256sum -c -; \
	tar -C /usr/local -xzf go.tgz; \
	rm go.tgz; \
	\
	if [ "$goRelArch" = 'src' ]; then \
		echo >&2; \
		echo >&2 'error: UNIMPLEMENTED'; \
		echo >&2 'TODO install golang-any from jessie-backports for GOROOT_BOOTSTRAP (and uninstall after build)'; \
		echo >&2; \
		exit 1; \
	fi; \
	\
	export PATH="$GOPATH/bin:$GOROOT/bin:$PATH"; \
#	export PATH="/usr/local/go/bin:$PATH"; \
	go version

COPY . "$GOPATH/src"
RUN mkdir -p "$GOPATH/src" "$GOPATH/bin" && chown -R executor:executor "$GOPATH" && chmod -R 750 "$GOPATH"
USER executor
WORKDIR $GOPATH
RUN cd "$GOPATH/src" && \
#    go vet ./... && \
    rm -rf gerrit-share.lan.crt server.crt Jenkinsfile Dockerfile && \
    go vet ./...;  if [ $? -eq 0 ]; then     echo GO VET SUCCESSFULL; else     echo GO VET FAILED; exit 1 ; fi ; \
    go get -v all ;
