Feature: REST contract rules from the documentation
  Derived from the shared errors of the use-cases page (ErrNotFound,
  ErrIllegalTransition), the Idempotency-Key rule ("every command commits")
  and the keyset paging of the query routes in apis/openapi.yaml. Driven
  through the real router over the in-memory doubles.

  Background:
    Given the region "SP" is defined

  Scenario Outline: A command on an unknown aggregate answers 404 not-found
    When a POST request is sent to "<path>" with the idempotency key "k-1"
    Then the response status is 404
    And the response problem is "not-found"

    Examples:
      | path                       |
      | /nodes/node-404/activations |
      | /nodes/node-404/retirements |
      | /lanes/lane-404/closures    |

  Scenario: Suspending an unknown node answers 404 not-found
    When a POST request is sent to "/nodes/node-404/suspensions" with the idempotency key "k-2" and the JSON body
      """json
      {"reason":"flooded dock"}
      """
    Then the response status is 404
    And the response problem is "not-found"

  Scenario: Declaring a profile on an unknown node answers 404 not-found
    When a PUT request is sent to "/nodes/node-404/operational-profile" with the idempotency key "k-3" and the JSON body
      """json
      {"profileRevision":1,"timezone":"America/Sao_Paulo","operatingHoursByDay":{"monday":{"start":"06:00","end":"22:00"}},"holidays":[],"handlingTimeMinutes":30,"cutoffs":[{"localTime":"11:00"}],"throughputCap":{"value":100,"timeUnit":"HOUR","unit":"ORDER"},"declaredBy":"NETWORK_OPS"}
      """
    Then the response status is 404
    And the response problem is "not-found"

  Scenario Outline: A command outside the transition table answers 409 illegal-transition
    Given a "DELIVERY_STATION" node "ds" is <state> in the region "SP"
    When a POST request is sent to "/nodes/node:ds/<route>" with the idempotency key "k-4"
    Then the response status is 409
    And the response problem is "illegal-transition"

    Examples:
      | state     | route       |
      | ACTIVE    | activations |
      | PROPOSED  | retirements |

  Scenario Outline: Suspending outside ACTIVE answers 409 illegal-transition
    Given a "DELIVERY_STATION" node "ds" is <state> in the region "SP"
    When a POST request is sent to "/nodes/node:ds/suspensions" with the idempotency key "k-5" and the JSON body
      """json
      {"reason":"flooded dock"}
      """
    Then the response status is 409
    And the response problem is "illegal-transition"
    And the node "ds" is <state>

    Examples:
      | state     |
      | PROPOSED  |
      | SUSPENDED |

  Scenario Outline: Every command on a RETIRED node answers 409 node-retired
    Given a "DELIVERY_STATION" node "ds" is RETIRED in the region "SP"
    When a POST request is sent to "/nodes/node:ds/<route>" with the idempotency key "k-6"
    Then the response status is 409
    And the response problem is "node-retired"

    Examples:
      | route       |
      | activations |
      | retirements |

  Scenario: Suspending a RETIRED node answers 409 node-retired
    Given a "DELIVERY_STATION" node "ds" is RETIRED in the region "SP"
    When a POST request is sent to "/nodes/node:ds/suspensions" with the idempotency key "k-7" and the JSON body
      """json
      {"reason":"flooded dock"}
      """
    Then the response status is 409
    And the response problem is "node-retired"

  Scenario: Closing a lane twice answers 409 illegal-transition
    Given an active "FULFILLMENT_CENTER" node "fc" exists in the region "SP"
    And an active "DELIVERY_STATION" node "ds" exists in the region "SP"
    And an open GROUND lane "L1" exists from "fc" to "ds"
    And I close the lane "L1"
    When a POST request is sent to "/lanes/lane:L1/closures" with the idempotency key "k-8"
    Then the response status is 409
    And the response problem is "illegal-transition"

  Scenario Outline: Every state-changing route requires an Idempotency-Key
    When a keyed <method> request is sent to "<path>"
    Then the response status is 400
    And the response problem is "idempotency-key-required"

    Examples:
      | method | path                                  |
      | POST   | /nodes                                |
      | PUT    | /nodes/node-1/operational-profile     |
      | POST   | /nodes/node-1/activations             |
      | POST   | /nodes/node-1/suspensions             |
      | POST   | /nodes/node-1/retirements             |
      | POST   | /lanes                                |
      | POST   | /lanes/lane-1/closures                |
      | PUT    | /service-areas/013                    |
      | POST   | /regions                              |

  Scenario: A read needs no Idempotency-Key
    Given the region "RJ" is defined
    When a keyed GET request is sent to "/regions"
    Then the response status is 200
    And the response list "items" has 2 items

  Scenario: An unknown node status is not a filter
    When a GET request is sent to "/nodes?status=DORMANT"
    Then the response status is 422
    And the response problem is "validation-failed"

  Scenario: A listing with no match is a 200 with an empty page and no next cursor
    When a GET request is sent to "/nodes?type=AIR_HUB"
    Then the response status is 200
    And the response list "items" has 0 items
    And the response has no field "nextCursor"

  Scenario: Nodes are paged by nodeId ascending, the cursor leads to the next page
    Given a "DELIVERY_STATION" node "ds-1" exists in the region "SP"
    And a "DELIVERY_STATION" node "ds-2" exists in the region "SP"
    And a "DELIVERY_STATION" node "ds-3" exists in the region "SP"
    And a "DELIVERY_STATION" node "ds-4" exists in the region "SP"
    And a "DELIVERY_STATION" node "ds-5" exists in the region "SP"
    When a GET request is sent to "/nodes?type=DELIVERY_STATION&limit=2"
    Then the response status is 200
    And the response list "items" has 2 items
    And the response field "items.0.nodeId" is "node:ds-1"
    And the response field "items.1.nodeId" is "node:ds-2"
    When the next cursor of the response is remembered
    And a GET request is sent to "/nodes?type=DELIVERY_STATION&limit=2" with the remembered cursor
    Then the response list "items" has 2 items
    And the response field "items.0.nodeId" is "node:ds-3"
    And the response field "items.1.nodeId" is "node:ds-4"
    When the next cursor of the response is remembered
    And a GET request is sent to "/nodes?type=DELIVERY_STATION&limit=2" with the remembered cursor
    Then the response list "items" has 1 items
    And the response field "items.0.nodeId" is "node:ds-5"
    And the response has no field "nextCursor"

  Scenario: A page that exactly exhausts the matches has no next cursor
    Given a "DELIVERY_STATION" node "ds-1" exists in the region "SP"
    And a "DELIVERY_STATION" node "ds-2" exists in the region "SP"
    When a GET request is sent to "/nodes?limit=2"
    Then the response list "items" has 2 items
    And the response has no field "nextCursor"

  Scenario: A page of one leaves a next cursor while matches remain
    Given a "DELIVERY_STATION" node "ds-1" exists in the region "SP"
    And a "DELIVERY_STATION" node "ds-2" exists in the region "SP"
    When a GET request is sent to "/nodes?limit=1"
    Then the response list "items" has 1 items
    And the response field "items.0.nodeId" is "node:ds-1"
    And the next cursor of the response is remembered

  Scenario Outline: The page size is between 1 and 500
    When a GET request is sent to "<path>?limit=<limit>"
    Then the response status is 400
    And the response problem is "invalid-limit"

    Examples:
      | path     | limit |
      | /nodes   | 0     |
      | /nodes   | 501   |
      | /nodes   | -1    |
      | /nodes   | many  |
      | /lanes   | 0     |
      | /regions | 501   |

  Scenario Outline: The largest and the smallest page size are accepted
    Given a "DELIVERY_STATION" node "ds-1" exists in the region "SP"
    When a GET request is sent to "/nodes?limit=<limit>"
    Then the response status is 200
    And the response list "items" has 1 items

    Examples:
      | limit |
      | 1     |
      | 500   |

  Scenario: A cursor that is not one is a 400 invalid-cursor
    When a GET request is sent to "/nodes?cursor=not-a-cursor"
    Then the response status is 400
    And the response problem is "invalid-cursor"

  Scenario Outline: A query parameter the collection does not declare is a 400
    When a GET request is sent to "<path>?<parameter>"
    Then the response status is 400
    And the response problem is "unknown-query-parameter"

    Examples:
      | path     | parameter      |
      | /nodes   | offset=10      |
      | /nodes   | after=node-1   |
      | /nodes   | from=node-1    |
      | /nodes   | nextToken=abc  |
      | /nodes   | color=blue     |
      | /lanes   | offset=10      |
      | /regions | page=2         |

  Scenario: Regions are paged with the same cursor
    Given the region "RJ" is defined
    When a GET request is sent to "/regions?limit=1"
    Then the response list "items" has 1 items
    When the next cursor of the response is remembered
    And a GET request is sent to "/regions?limit=1" with the remembered cursor
    Then the response list "items" has 1 items
    And the response has no field "nextCursor"

  Scenario: Lanes are paged with the same cursor
    Given an active "FULFILLMENT_CENTER" node "fc" exists in the region "SP"
    And an active "SORTATION_CENTER" node "sc" exists in the region "SP"
    And an active "DELIVERY_STATION" node "ds" exists in the region "SP"
    And an open GROUND lane "L1" exists from "fc" to "sc"
    And an open GROUND lane "L2" exists from "fc" to "ds"
    When a GET request is sent to "/lanes?origin=node:fc&limit=1"
    Then the response list "items" has 1 items
    When the next cursor of the response is remembered
    And a GET request is sent to "/lanes?origin=node:fc&limit=1" with the remembered cursor
    Then the response list "items" has 1 items
    And the response has no field "nextCursor"
