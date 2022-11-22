FROM quay.io/operator-framework/upstream-opm-builder
LABEL operators.operatorframework.io.index.database.v1=/database/index.db
ADD database/index.db /database/index.db
RUN mkdir /registry && chmod 775 /registry
EXPOSE 50051
WORKDIR /registry
ENTRYPOINT ["/bin/opm"]
CMD ["registry", "serve", "--database", "/database/index.db"]
