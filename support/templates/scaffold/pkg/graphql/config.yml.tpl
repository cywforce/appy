# Refer to https://gqlgen.com/config/ for detailed config.yml documentation.

schema:
  - pkg/graphql/schema/*.gql
  - pkg/graphql/schema/*.graphql

exec:
  filename: pkg/graphql/generated/generated.go

model:
  filename: pkg/graphql/model/models_gen.go

resolver:
  filename: pkg/graphql/graphql.go
  type: ResolverRoot
