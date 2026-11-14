Feature: Node registration rules from the documentation
  Derived from the RegisterNode use case, the node-types table of the business
  context and the Node invariants: capabilities come from the type, a subtype
  belongs to its type, the vendor node code is unique per vendor across every
  Node aggregate, and the NodeRegistered event carries what the consumers
  build their read models from.

  Background:
    Given the region "SP" is defined

  Scenario Outline: Capabilities are exactly those of the node type
    Given a "<type>" node "n-1" exists in the region "SP"
    Then the node "n-1" has exactly the capabilities "<capabilities>"

    Examples:
      | type               | capabilities     |
      | FULFILLMENT_CENTER | inbound,outbound |
      | INBOUND_CROSS_DOCK | inbound          |
      | RETURNS_CENTER     | returns          |
      | SORTATION_CENTER   | sort             |
      | DELIVERY_STATION   | lastMile         |
      | AIR_HUB            | air,sort         |

  Scenario Outline: Every subtype of the fulfillment center is accepted
    When I register a "FULFILLMENT_CENTER" node "fc-1" with the subtype "<subtype>" in the region "SP"
    Then the command succeeds
    And the last command published a "NodeRegistered" event whose "Subtype" is "<subtype>"

    Examples:
      | subtype      |
      | SORTABLE     |
      | NON_SORTABLE |
      | XL           |
      | SOFT_LINES   |
      | SUB_SAME_DAY |
      | FRESH        |
      | MICRO        |

  Scenario Outline: A node type other than the fulfillment center has no subtypes
    When I register a "<type>" node "n-1" with the subtype "SORTABLE" in the region "SP"
    Then the command is refused with "subtype-not-allowed"
    And the last command published no events
    And nothing was stored by the last command

    Examples:
      | type               |
      | INBOUND_CROSS_DOCK |
      | RETURNS_CENTER     |
      | SORTATION_CENTER   |
      | AIR_HUB            |

  Scenario: A subtype outside the fulfillment center's list is refused
    When I register a "FULFILLMENT_CENTER" node "fc-1" with the subtype "GIGANTIC" in the region "SP"
    Then the command is refused with "subtype-not-allowed"

  Scenario: A registration starts PROPOSED and publishes exactly NodeRegistered
    When I register a "DELIVERY_STATION" node "ds-1" in the region "SP"
    Then the command succeeds
    And the last command published the event "NodeRegistered"
    And the node "ds-1" is PROPOSED

  Scenario: The registered vendor node carries its vendor reference on the event
    When the vendor "V-WS" registers a "FULFILLMENT_CENTER" node "fc-7" with the vendor code "FC-7" in the region "SP"
    Then the last command published a "NodeRegistered" event whose "NodeType" is "FULFILLMENT_CENTER"
    And the last command published a "NodeRegistered" event whose "Operator" is "VENDOR"
    And the last command published a "NodeRegistered" event whose "VendorID" is "V-WS"
    And the last command published a "NodeRegistered" event whose "VendorNodeCode" is "FC-7"
    And the last command published a "NodeRegistered" event whose "RegionID" is "region:SP"

  Scenario: A network node carries no vendor reference on the event
    When I register a "AIR_HUB" node "ah-1" operated by "NETWORK" in the region "SP"
    Then the last command published a "NodeRegistered" event whose "Operator" is "NETWORK"
    And the last command published a "NodeRegistered" event whose "VendorID" is ""
    And the last command published a "NodeRegistered" event whose "VendorNodeCode" is ""

  Scenario: The event carries the building's time zone and postal address
    When I register a "DELIVERY_STATION" node "ds-1" in the region "SP"
    Then the last command published a "NodeRegistered" event whose "Timezone" is "America/Sao_Paulo"
    And the last command published a "NodeRegistered" event whose "PostalCode" is "01310"
    And the last command published a "NodeRegistered" event whose "CountryCode" is "BR"

  Scenario: The vendor node code is unique per vendor across every region
    Given the region "RJ" is defined
    And the vendor "V-WS" registers a "FULFILLMENT_CENTER" node "fc-1" with the vendor code "FC-1" in the region "SP"
    When the vendor "V-WS" registers a "FULFILLMENT_CENTER" node "fc-2" with the vendor code "FC-1" in the region "RJ"
    Then the command is refused with "vendor-node-code-taken"
    And the last command published no events

  Scenario: The vendor node code is unique across node types of the same vendor
    Given the vendor "V-WS" registers a "FULFILLMENT_CENTER" node "fc-1" with the vendor code "SITE-1" in the region "SP"
    When the vendor "V-WS" registers a "RETURNS_CENTER" node "rc-1" with the vendor code "SITE-1" in the region "SP"
    Then the command is refused with "vendor-node-code-taken"

  Scenario: A retired node keeps its vendor node code
    Given a "FULFILLMENT_CENTER" node "fc-1" is RETIRED in the region "SP"
    When the vendor "vendor-1" registers a "FULFILLMENT_CENTER" node "fc-2" with the vendor code "fc-1" in the region "SP"
    Then the command is refused with "vendor-node-code-taken"

  Scenario: A refused registration leaves no node behind
    Given the vendor "V-WS" registers a "FULFILLMENT_CENTER" node "fc-1" with the vendor code "FC-1" in the region "SP"
    When the vendor "V-WS" registers a "FULFILLMENT_CENTER" node "fc-2" with the vendor code "FC-1" in the region "SP"
    Then the command is refused with "vendor-node-code-taken"
    And listing nodes with the type "FULFILLMENT_CENTER", status "" and region "" returns "fc-1"

  Scenario Outline: A vendor never registers a parcel node, whatever its region
    Given the region "RJ" is defined
    When I register a "<type>" node "n-1" operated by "VENDOR" in the region "RJ"
    Then the command is refused with "operator-not-allowed-for-node-type"
    And the last command published no events

    Examples:
      | type             |
      | SORTATION_CENTER |
      | DELIVERY_STATION |
      | AIR_HUB          |

  Scenario Outline: The network may not run a vendor's supply-side node as a NETWORK node
    When I register a "<type>" node "n-1" operated by "NETWORK" in the region "SP"
    Then the command is refused with "operator-not-allowed-for-node-type"
    And nothing was stored by the last command

    Examples:
      | type               |
      | FULFILLMENT_CENTER |
      | INBOUND_CROSS_DOCK |
      | RETURNS_CENTER     |
