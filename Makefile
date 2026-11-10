SHELL := /bin/bash

BUILD_PATH	?= build
PRIVATE_KEY	?= $(BUILD_PATH)/private_key.pem
PUBLIC_KEY	?= $(BUILD_PATH)/public_key.pem

.PHONY: keys
keys  : $(PRIVATE_KEY) $(PUBLIC_KEY)

.PHONY: clean
clean :
	@rm -rf $(BUILD_PATH)

.PHONY: test
test  :
	go test -v ./...

.PHONY     : integration
integration:
	go test -v -tags=integration ./...

# Helpers.

$(PRIVATE_KEY):
	@mkdir -p $(dir $@)
	@openssl genpkey -algorithm ed25519 -out $@
	@chmod 600 $@

$(PUBLIC_KEY): $(PRIVATE_KEY)
	@mkdir -p $(dir $@)
	@openssl pkey -in $< -pubout -out $@
