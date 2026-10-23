Feature: Idempotency-Key guard
  Every state-changing route requires an Idempotency-Key header. These
  scenarios drive the REAL middleware; its request validation runs before
  it would open a transaction, so no store is needed here. The replay
  semantics (same key replays the stored response, another fingerprint is
  a 422) need the idempotency_keys table and live in the integration suite
  (internal/adapters/inbound/http/idempotency_integration_test.go).

  Scenario: A state-changing request without a key is a 400 problem
    When a keyed POST request is sent to "/nodes" with the JSON body
      """json
      {"nodeType":"FULFILLMENT_CENTER","operator":"NETWORK","regionId":"region-1","timezone":"America/Sao_Paulo","location":{"postalCode":"01310-000","countryCode":"BR"}}
      """
    Then the response status is 400
    And the response problem is "idempotency-key-required"

  Scenario: A key past the length limit is a 400 problem
    When a keyed POST request is sent to "/regions" with the idempotency key "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk" and the JSON body
      """json
      {"code":"RJ","name":"Rio de Janeiro"}
      """
    Then the response status is 400
    And the response problem is "idempotency-key-invalid"
