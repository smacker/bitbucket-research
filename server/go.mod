module github.com/smacker/bitbucket-research/server

go 1.12

replace github.com/gfleury/go-bitbucket-v1 => github.com/smacker/go-bitbucket-v1 v0.0.0-20191003203704-cd6c0379d76e

require (
	github.com/gfleury/go-bitbucket-v1 v0.0.0-20190725203704-66612acc3762
	github.com/golang-migrate/migrate/v4 v4.4.0
	github.com/lib/pq v1.2.0
	github.com/mitchellh/mapstructure v1.1.2
	github.com/src-d/metadata-retrieval v0.0.0-20190930135144-01b82fabac42
	github.com/wbrefvem/go-bitbucket v0.0.0-20190128183802-fc08fd046abb
	gopkg.in/src-d/go-log.v1 v1.0.2
)
