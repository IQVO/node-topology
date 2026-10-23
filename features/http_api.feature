Feature: REST adapter
  The wire face of every operation of apis/openapi.yaml: status codes, RFC
  9457 problem slugs and response fields, driven through the real router
  over the same in-memory doubles the use-case scenarios use. The
  transactional Idempotency-Key store needs Postgres and lives in the
  integration suite; here the middleware is a pass-through double (its
  request validation is covered by http_idempotency.feature).

  Background:
    Given the region "SP" is defined

  Scenario: Registering a node over REST
    When a POST request is sent to "/nodes" with the idempotency key "reg-1" and the JSON body
      """json
      {"nodeType":"DELIVERY_STATION","operator":"NETWORK","regionId":"region:SP","timezone":"America/Sao_Paulo","location":{"postalCode":"01310-000","countryCode":"BR"}}
      """
    Then the response status is 201
    And the response field "nodeType" is "DELIVERY_STATION"
    And the response field "status" is "PROPOSED"
    And the response field "regionId" is "region:SP"
    And the node id of the response is remembered as "fc"
    When a GET request is sent to "/nodes/node:fc"
    Then the response status is 200
    And the response field "nodeId" is "node:fc"
    And the response field "timezone" is "America/Sao_Paulo"
    And the response has no field "operationalProfile"

  Scenario: Reading an unknown node is a 404 problem
    When a GET request is sent to "/nodes/node-404"
    Then the response status is 404
    And the response problem is "not-found"

  Scenario: A malformed body is a 400 problem
    When a POST request is sent to "/nodes" with the raw body
      """
      {"nodeType":
      """
    Then the response status is 400
    And the response problem is "malformed-request-body"

  Scenario: An unknown field is a 400 problem
    When a POST request is sent to "/nodes" with the idempotency key "reg-2" and the JSON body
      """json
      {"nodeType":"DELIVERY_STATION","operator":"NETWORK","regionId":"region:SP","timezone":"America/Sao_Paulo","location":{"postalCode":"01310-000","countryCode":"BR"},"capacity":9}
      """
    Then the response status is 400
    And the response problem is "malformed-request-body"

  Scenario: An unknown region is a 422 problem
    When a POST request is sent to "/nodes" with the idempotency key "reg-3" and the JSON body
      """json
      {"nodeType":"DELIVERY_STATION","operator":"NETWORK","regionId":"region-999","timezone":"America/Sao_Paulo","location":{"postalCode":"01310-000","countryCode":"BR"}}
      """
    Then the response status is 422
    And the response problem is "unknown-region"

  Scenario: A vendor code taken twice is a 409 problem
    Given the vendor "v1" registers a "FULFILLMENT_CENTER" node "vfc" with the vendor code "C1" in the region "SP"
    When a POST request is sent to "/nodes" with the idempotency key "reg-4" and the JSON body
      """json
      {"nodeType":"FULFILLMENT_CENTER","operator":"VENDOR","vendorRef":{"vendorId":"v1","vendorNodeCode":"C1"},"regionId":"region:SP","timezone":"America/Sao_Paulo","location":{"postalCode":"01310-000","countryCode":"BR"}}
      """
    Then the response status is 409
    And the response problem is "vendor-node-code-taken"

  Scenario: Nodes are listed through the query
    Given an active "FULFILLMENT_CENTER" node "fc" exists in the region "SP"
    When a GET request is sent to "/nodes?type=FULFILLMENT_CENTER&status=ACTIVE&region=region:SP"
    Then the response status is 200
    And the response list "nodes" has 1 items
    When a GET request is sent to "/nodes?type=DELIVERY_STATION"
    Then the response status is 200
    And the response list "nodes" has 0 items

  Scenario: An unknown node type is not a filter
    When a GET request is sent to "/nodes?type=NOT_A_TYPE"
    Then the response status is 422
    And the response problem is "validation-failed"

  Scenario: Declaring a profile over REST
    Given a "FULFILLMENT_CENTER" node "fc" exists in the region "SP"
    When a PUT request is sent to "/nodes/node:fc/operational-profile" with the idempotency key "prof-1" and the JSON body
      """json
      {"profileRevision":1,"timezone":"America/Sao_Paulo","operatingHoursByDay":{"monday":{"start":"06:00","end":"22:00"}},"holidays":["2026-12-25"],"handlingTimeMinutes":30,"cutoffs":[{"localTime":"11:00"}],"throughputCap":{"value":100,"timeUnit":"HOUR","unit":"ORDER"},"declaredBy":"NETWORK_OPS"}
      """
    Then the response status is 200
    And the response field "operationalProfile.profileRevision" is "1"
    And the response field "operationalProfile.cutoffs.0.localTime" is "11:00"

  Scenario: Repeating a revision over REST is a 409 problem
    Given a "FULFILLMENT_CENTER" node "fc" exists in the region "SP"
    And the node "fc" has declared a complete profile
    When a PUT request is sent to "/nodes/node:fc/operational-profile" with the idempotency key "prof-2" and the JSON body
      """json
      {"profileRevision":1,"timezone":"America/Sao_Paulo","operatingHoursByDay":{"monday":{"start":"06:00","end":"22:00"}},"holidays":["2026-12-25"],"handlingTimeMinutes":30,"cutoffs":[{"localTime":"11:00"}],"throughputCap":{"value":100,"timeUnit":"HOUR","unit":"ORDER"},"declaredBy":"NETWORK_OPS"}
      """
    Then the response status is 409
    And the response problem is "stale-profile-revision"

  Scenario: A cutoff outside the operating hours is a 422 problem
    Given a "FULFILLMENT_CENTER" node "fc" exists in the region "SP"
    When a PUT request is sent to "/nodes/node:fc/operational-profile" with the idempotency key "prof-3" and the JSON body
      """json
      {"profileRevision":1,"timezone":"America/Sao_Paulo","operatingHoursByDay":{"monday":{"start":"06:00","end":"22:00"}},"holidays":[],"handlingTimeMinutes":30,"cutoffs":[{"localTime":"23:00","days":["monday"]}],"throughputCap":{"value":100,"timeUnit":"HOUR","unit":"ORDER"},"declaredBy":"NETWORK_OPS"}
      """
    Then the response status is 422
    And the response problem is "cutoff-outside-operating-hours"

  Scenario: Activating a fulfillment center over REST
    Given a "FULFILLMENT_CENTER" node "fc" exists in the region "SP"
    When a POST request is sent to "/nodes/node:fc:activate" with the idempotency key "act-1"
    Then the response status is 409
    And the response problem is "incomplete-operational-profile"
    Given the node "fc" has declared a complete profile
    When a POST request is sent to "/nodes/node:fc:activate" with the idempotency key "act-2"
    Then the response status is 200
    And the response field "status" is "ACTIVE"

  Scenario: Suspending over REST
    Given an active "FULFILLMENT_CENTER" node "fc" exists in the region "SP"
    When a POST request is sent to "/nodes/node:fc:suspend" with the idempotency key "sus-1" and the JSON body
      """json
      {"reason":" "}
      """
    Then the response status is 422
    And the response problem is "validation-failed"
    When a POST request is sent to "/nodes/node:fc:suspend" with the idempotency key "sus-2" and the JSON body
      """json
      {"reason":"flooded dock"}
      """
    Then the response status is 200
    And the response field "status" is "SUSPENDED"
    And the response field "suspendReason" is "flooded dock"

  Scenario: Retirement is terminal over REST
    Given an active "DELIVERY_STATION" node "ds" exists in the region "SP"
    When a POST request is sent to "/nodes/node:ds:retire" with the idempotency key "ret-1"
    Then the response status is 200
    And the response field "status" is "RETIRED"
    When a POST request is sent to "/nodes/node:ds:activate" with the idempotency key "ret-2"
    Then the response status is 409
    And the response problem is "node-retired"

  Scenario: Opening a lane over REST
    Given an active "FULFILLMENT_CENTER" node "fc" exists in the region "SP"
    And an active "DELIVERY_STATION" node "ds" exists in the region "SP"
    When a POST request is sent to "/lanes" with the idempotency key "lane-1" and the JSON body
      """json
      {"originNodeId":"node:fc","destinationNodeId":"node:ds","mode":"GROUND","nominalTransitMinutes":90}
      """
    Then the response status is 201
    And the response field "mode" is "GROUND"
    And the response field "status" is "OPEN"
    And the response field "nominalTransitMinutes" is "90"
    And the lane id of the response is remembered as "L1"
    When a GET request is sent to "/lanes?origin=node:fc"
    Then the response status is 200
    And the response list "lanes" has 1 items
    And the response field "lanes.0.laneId" is "lane:L1"

  Scenario: A duplicate lane over REST is a 409 problem
    Given an active "FULFILLMENT_CENTER" node "fc" exists in the region "SP"
    And an active "DELIVERY_STATION" node "ds" exists in the region "SP"
    And an open GROUND lane "L1" exists from "fc" to "ds"
    When a POST request is sent to "/lanes" with the idempotency key "lane-2" and the JSON body
      """json
      {"originNodeId":"node:fc","destinationNodeId":"node:ds","mode":"GROUND","nominalTransitMinutes":45}
      """
    Then the response status is 409
    And the response problem is "duplicate-lane"

  Scenario: A lane cannot loop over REST
    Given an active "SORTATION_CENTER" node "sc" exists in the region "SP"
    When a POST request is sent to "/lanes" with the idempotency key "lane-3" and the JSON body
      """json
      {"originNodeId":"node:sc","destinationNodeId":"node:sc","mode":"GROUND","nominalTransitMinutes":10}
      """
    Then the response status is 422
    And the response problem is "lane-loop"

  Scenario: An unknown lane end is a 404 problem
    Given an active "FULFILLMENT_CENTER" node "fc" exists in the region "SP"
    When a POST request is sent to "/lanes" with the idempotency key "lane-4" and the JSON body
      """json
      {"originNodeId":"node:fc","destinationNodeId":"node-404","mode":"GROUND","nominalTransitMinutes":30}
      """
    Then the response status is 404
    And the response problem is "not-found"

  Scenario: Closing a lane over REST
    Given an active "FULFILLMENT_CENTER" node "fc" exists in the region "SP"
    And an active "DELIVERY_STATION" node "ds" exists in the region "SP"
    And an open GROUND lane "L1" exists from "fc" to "ds"
    When a POST request is sent to "/lanes/lane:L1:close" with the idempotency key "close-1"
    Then the response status is 200
    And the response field "status" is "CLOSED"

  Scenario: Assigning a service area over REST
    Given an active "DELIVERY_STATION" node "ds" exists in the region "SP"
    When a PUT request is sent to "/service-areas/013" with the idempotency key "area-1" and the JSON body
      """json
      {"deliveryStationNodeId":"node:ds","assignmentVersion":1}
      """
    Then the response status is 200
    And the response field "postalPrefix" is "013"
    And the response field "deliveryStationNodeId" is "node:ds"
    And the response field "regionId" is "region:SP"
    And the response field "assignmentVersion" is "1"
    When a GET request is sent to "/service-areas/013"
    Then the response status is 200
    And the response field "deliveryStationNodeId" is "node:ds"

  Scenario: A stale assignment version over REST is a 409 problem
    Given an active "DELIVERY_STATION" node "ds" exists in the region "SP"
    And the postal prefix "013" is assigned to the station "ds" with assignment version 1
    When a PUT request is sent to "/service-areas/013" with the idempotency key "area-2" and the JSON body
      """json
      {"deliveryStationNodeId":"node:ds","assignmentVersion":1}
      """
    Then the response status is 409
    And the response problem is "stale-assignment-version"

  Scenario: An unknown prefix is a 404 problem
    When a GET request is sent to "/service-areas/99999"
    Then the response status is 404
    And the response problem is "not-found"

  Scenario: Defining a region over REST
    When a POST request is sent to "/regions" with the idempotency key "region-1" and the JSON body
      """json
      {"code":"RJ","name":"Rio de Janeiro"}
      """
    Then the response status is 201
    And the response field "code" is "RJ"
    And the response field "name" is "Rio de Janeiro"
    And the region id of the response is remembered as "RJ"
    When a GET request is sent to "/regions"
    Then the response status is 200
    And the response list "" has 2 items
    When a GET request is sent to "/regions/region:RJ"
    Then the response status is 200
    And the response field "code" is "RJ"

  Scenario: A duplicate region code over REST is a 409 problem
    When a POST request is sent to "/regions" with the idempotency key "region-2" and the JSON body
      """json
      {"code":"SP","name":"Sao Paulo again"}
      """
    Then the response status is 409
    And the response problem is "region-code-taken"

  Scenario: A malformed region code over REST is a 422 problem
    When a POST request is sent to "/regions" with the idempotency key "region-3" and the JSON body
      """json
      {"code":"south east","name":"Nope"}
      """
    Then the response status is 422
    And the response problem is "validation-failed"

  Scenario: An unknown route is a 404 problem
    When a GET request is sent to "/nope"
    Then the response status is 404
    And the response problem is "not-found"

  Scenario: An unsupported method is a 405 problem
    When a DELETE request is sent to "/nodes/node-1"
    Then the response status is 405
    And the response problem is "method-not-allowed"

  Scenario: The probes answer
    When a GET request is sent to "/healthz"
    Then the response status is 200
    When a GET request is sent to "/readyz"
    Then the response status is 200
