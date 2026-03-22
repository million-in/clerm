# Shopify Compiler Checks

This is a compiler-only CLERM flow for the Shopify schema. It does not use repository unit tests or a running resolver server.

## 1. Build the compiler

```bash
cd /Users/tigre/Desktop/clerm
make build
eval "$(bin/clerm shellenv)"
```

## 2. Compile the Shopify schema

```bash
mkdir -p schemas
clerm compile -in examples/shopify.clermfile -out schemas/shopify.clermcfg
```

## 3. Inspect the compiled schema

```bash
clerm inspect -in schemas/shopify.clermcfg
clerm inspect -in schemas/shopify.clermcfg -internal
```

## 4. Show callable tools

```bash
clerm tools -schema schemas/shopify.clermcfg -allow @global
clerm tools -schema schemas/shopify.clermcfg -allow @verified
```

## 5. Generate local keys for verified request tests

```bash
clerm token keygen -out-private dev.ed25519 -out-public dev.ed25519.pub
```

## 6. Build public requests

```bash
clerm request \
  -schema schemas/shopify.clermcfg \
  -method @global.shopify.search_merchants.v1 \
  -allow @global \
  -data-file examples/shopify.search_merchants.payload \
  -out search_merchants.clerm

clerm request \
  -schema schemas/shopify.clermcfg \
  -method @global.shopify.search_products.v2 \
  -allow @global \
  -data-file examples/shopify.search_products.payload \
  -out search_products.clerm
```

## 7. Issue verified tokens

```bash
clerm token issue \
  -schema schemas/shopify.clermcfg \
  -method @verified.shopify.buy_products.v1 \
  -issuer registry \
  -subject buyer-42 \
  -private-key dev.ed25519 \
  -out buy_products.token

clerm token issue \
  -schema schemas/shopify.clermcfg \
  -method @verified.shopify.make_store.v1 \
  -issuer registry \
  -subject merchant-42 \
  -private-key dev.ed25519 \
  -out make_store.token

clerm token issue \
  -schema schemas/shopify.clermcfg \
  -method @verified.shopify.check_balance.v3 \
  -issuer registry \
  -subject account-42 \
  -private-key dev.ed25519 \
  -out check_balance.token
```

## 8. Build verified requests

```bash
clerm request \
  -schema schemas/shopify.clermcfg \
  -method @verified.shopify.buy_products.v1 \
  -allow @verified \
  -data-file examples/shopify.buy_products.payload \
  -cap-file buy_products.token \
  -out buy_products.clerm

clerm request \
  -schema schemas/shopify.clermcfg \
  -method @verified.shopify.make_store.v1 \
  -allow @verified \
  -data-file examples/shopify.make_store.payload \
  -cap-file make_store.token \
  -out make_store.clerm

clerm request \
  -schema schemas/shopify.clermcfg \
  -method @verified.shopify.check_balance.v3 \
  -allow @verified \
  -data-file examples/shopify.check_balance.payload \
  -cap-file check_balance.token \
  -out check_balance.clerm
```

## 9. Inspect generated requests

```bash
clerm inspect -in search_merchants.clerm
clerm inspect -in search_products.clerm
clerm inspect -in buy_products.clerm
clerm inspect -in make_store.clerm
clerm inspect -in check_balance.clerm
```

## 10. Resolve offline

```bash
clerm resolve \
  -schema schemas/shopify.clermcfg \
  -request search_merchants.clerm \
  -target registry.discover

clerm resolve \
  -schema schemas/shopify.clermcfg \
  -request search_products.clerm \
  -target registry.discover

clerm resolve \
  -schema schemas/shopify.clermcfg \
  -request buy_products.clerm \
  -target registry.invoke \
  -cap-public-key dev.ed25519.pub

clerm resolve \
  -schema schemas/shopify.clermcfg \
  -request make_store.clerm \
  -target registry.invoke \
  -cap-public-key dev.ed25519.pub

clerm resolve \
  -schema schemas/shopify.clermcfg \
  -request check_balance.clerm \
  -target registry.invoke \
  -cap-public-key dev.ed25519.pub
```
