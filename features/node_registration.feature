Feature: Node registration
  A node is one physical building of the network. It starts PROPOSED, its
  capabilities come from its type, parcel nodes are run by the network only and
  a vendor's own node code is unique per vendor.

  Background:
    Given the region "SP" is defined

  Scenario: Registering a vendor-run fulfillment center
    When I register a "FULFILLMENT_CENTER" node "fc-1" in the region "SP"
    Then the command succeeds
    And the last command published the event "NodeRegistered"
    And the node "fc-1" is PROPOSED
    And the node "fc-1" is run by "VENDOR"
    And the node "fc-1" has no profile

  Scenario Outline: A vendor may run only the supply-side node types
    When I register a "<type>" node "n-1" operated by "VENDOR" in the region "SP"
    Then the command succeeds
    And the node "n-1" is run by "VENDOR"

    Examples:
      | type               |
      | FULFILLMENT_CENTER |
      | INBOUND_CROSS_DOCK |
      | RETURNS_CENTER     |

  Scenario Outline: Parcel nodes are run by the network only
    When I register a "<type>" node "n-1" operated by "VENDOR" in the region "SP"
    Then the command is refused with "operator-not-allowed-for-node-type"
    And nothing was stored by the last command

    Examples:
      | type             |
      | SORTATION_CENTER |
      | DELIVERY_STATION |
      | AIR_HUB          |

  Scenario Outline: The network may run every parcel node type
    When I register a "<type>" node "n-1" operated by "NETWORK" in the region "SP"
    Then the command succeeds
    And the node "n-1" is run by "NETWORK"

    Examples:
      | type             |
      | SORTATION_CENTER |
      | DELIVERY_STATION |
      | AIR_HUB          |

  Scenario: A vendor node code is unique per vendor
    Given a "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    When the vendor "vendor-1" registers a "FULFILLMENT_CENTER" node "fc-2" with the vendor code "fc-1" in the region "SP"
    Then the command is refused with "vendor-node-code-taken"
    And nothing was stored by the last command

  Scenario: The same node code under another vendor is accepted
    Given a "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    When the vendor "vendor-2" registers a "FULFILLMENT_CENTER" node "fc-2" with the vendor code "fc-1" in the region "SP"
    Then the command succeeds

  Scenario: Network-run nodes have no vendor code to collide on
    Given a "DELIVERY_STATION" node "ds-1" exists in the region "SP"
    When I register a "DELIVERY_STATION" node "ds-2" in the region "SP"
    Then the command succeeds

  Scenario: A node needs a defined region
    When I register a "DELIVERY_STATION" node "ds-1" in an unknown region
    Then the command is refused with "unknown-region"
    And nothing was stored by the last command

  Scenario: A subtype must belong to the node type
    When I register a "DELIVERY_STATION" node "ds-1" with the subtype "SORTABLE" in the region "SP"
    Then the command is refused with "subtype-not-allowed"

  Scenario: A fulfillment center may carry one of its subtypes
    When I register a "FULFILLMENT_CENTER" node "fc-1" with the subtype "SORTABLE" in the region "SP"
    Then the command succeeds

  Scenario: Capabilities are derived from the node type
    Given a "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    And a "DELIVERY_STATION" node "ds-1" exists in the region "SP"
    Then the node "fc-1" has the capability "outbound"
    And the node "fc-1" lacks the capability "lastMile"
    And the node "ds-1" has the capability "lastMile"
    And the node "ds-1" lacks the capability "outbound"

  Scenario: Listing nodes by type, status and region
    Given the region "RJ" is defined
    And a "DELIVERY_STATION" node "ds-b" exists in the region "SP"
    And an active "DELIVERY_STATION" node "ds-a" exists in the region "SP"
    And an active "DELIVERY_STATION" node "ds-c" exists in the region "RJ"
    And a "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    Then listing nodes with the type "DELIVERY_STATION", status "" and region "" returns "ds-a, ds-b, ds-c"
    And listing nodes with the type "", status "ACTIVE" and region "" returns "ds-a, ds-c"
    And listing nodes with the type "DELIVERY_STATION", status "ACTIVE" and region "SP" returns "ds-a"
    And listing nodes with the type "AIR_HUB", status "" and region "" returns ""

  Scenario: An unknown node type is not a filter
    When listing nodes with the type "WAREHOUSE" fails as invalid
    Then the command is refused with "validation-failed"
