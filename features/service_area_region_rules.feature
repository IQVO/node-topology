Feature: Service area and region rules from the documentation
  Derived from the AssignServiceArea and DefineRegion use cases and the
  ServiceArea and Region canvases: the checks run in the order of the main
  flow (station type and state, then version), every saved version publishes
  one ServiceAreaAssigned with the station's region, prefixes version
  independently, and nothing unassigns a prefix or removes a region.

  Background:
    Given the region "SP" is defined
    And the region "RJ" is defined
    And an active "DELIVERY_STATION" node "ds-sp" exists in the region "SP"
    And an active "DELIVERY_STATION" node "ds-rj" exists in the region "RJ"

  Scenario: ServiceAreaAssigned carries the prefix, the station, its region and the version
    When I assign the postal prefix "013" to the station "ds-rj" with assignment version 4
    Then the last command published a "ServiceAreaAssigned" event whose "PostalPrefix" is "013"
    And the last command published a "ServiceAreaAssigned" event whose "DeliveryStationNodeID" is "node:ds-rj"
    And the last command published a "ServiceAreaAssigned" event whose "RegionID" is "region:RJ"
    And the last command published a "ServiceAreaAssigned" event whose "AssignmentVersion" is "4"

  Scenario: The region of the event follows the station on a reassignment
    Given the postal prefix "013" is assigned to the station "ds-sp" with assignment version 1
    When I assign the postal prefix "013" to the station "ds-rj" with assignment version 2
    Then the last command published a "ServiceAreaAssigned" event whose "RegionID" is "region:RJ"
    And the last command published a "ServiceAreaAssigned" event whose "DeliveryStationNodeID" is "node:ds-rj"

  Scenario: Every saved version publishes one event, skipped versions included
    Given the postal prefix "013" is assigned to the station "ds-sp" with assignment version 1
    And the postal prefix "013" is assigned to the station "ds-rj" with assignment version 2
    When I assign the postal prefix "013" to the station "ds-sp" with assignment version 9
    Then the outbox holds 3 "ServiceAreaAssigned" events in total
    And the postal prefix "013" resolves to the station "ds-sp" in the region "SP" with assignment version 9

  Scenario: A refused version publishes no event
    Given the postal prefix "013" is assigned to the station "ds-sp" with assignment version 5
    When I assign the postal prefix "013" to the station "ds-rj" with assignment version 5
    Then the command is refused with "stale-assignment-version"
    And the last command published no events
    And the outbox holds 1 "ServiceAreaAssigned" event in total

  Scenario: The same station may be assigned again under a higher version
    Given the postal prefix "013" is assigned to the station "ds-sp" with assignment version 1
    When I assign the postal prefix "013" to the station "ds-sp" with assignment version 2
    Then the command succeeds
    And the postal prefix "013" resolves to the station "ds-sp" in the region "SP" with assignment version 2

  Scenario: A new prefix may start at any version above zero
    When I assign the postal prefix "013" to the station "ds-sp" with assignment version 7
    Then the command succeeds
    And the postal prefix "013" resolves to the station "ds-sp" in the region "SP" with assignment version 7

  Scenario: Every prefix has its own assignment version
    Given the postal prefix "013" is assigned to the station "ds-sp" with assignment version 5
    When I assign the postal prefix "014" to the station "ds-rj" with assignment version 1
    Then the command succeeds
    And the postal prefix "013" resolves to the station "ds-sp" in the region "SP" with assignment version 5
    And the postal prefix "014" resolves to the station "ds-rj" in the region "RJ" with assignment version 1

  Scenario: One station may serve many prefixes
    Given the postal prefix "013" is assigned to the station "ds-sp" with assignment version 1
    When I assign the postal prefix "014" to the station "ds-sp" with assignment version 1
    Then the command succeeds
    And the postal prefix "013" resolves to the station "ds-sp" in the region "SP" with assignment version 1
    And the postal prefix "014" resolves to the station "ds-sp" in the region "SP" with assignment version 1

  Scenario: A retired station serves nothing new
    Given a "DELIVERY_STATION" node "ds-old" is RETIRED in the region "SP"
    When I assign the postal prefix "013" to the station "ds-old" with assignment version 1
    Then the command is refused with "not-a-delivery-station"
    And the postal prefix "013" is not assigned

  Scenario Outline: Only a delivery station can be assigned, whatever else the node is
    Given an active "<type>" node "n-1" exists in the region "SP"
    When I assign the postal prefix "013" to the station "n-1" with assignment version 1
    Then the command is refused with "not-a-delivery-station"
    And the last command published no events

    Examples:
      | type               |
      | FULFILLMENT_CENTER |
      | INBOUND_CROSS_DOCK |
      | RETURNS_CENTER     |
      | AIR_HUB            |

  Scenario: The station is checked before the version
    Given the postal prefix "013" is assigned to the station "ds-sp" with assignment version 5
    And I suspend the node "ds-rj" because "audit"
    When I assign the postal prefix "013" to the station "ds-rj" with assignment version 2
    Then the command is refused with "not-a-delivery-station"

  Scenario: A suspended station keeps the prefix it already serves
    Given the postal prefix "013" is assigned to the station "ds-sp" with assignment version 1
    When I suspend the node "ds-sp" because "audit"
    Then the postal prefix "013" resolves to the station "ds-sp" in the region "SP" with assignment version 1
    And the outbox holds 1 "ServiceAreaAssigned" event in total

  Scenario: A prefix reassigned away from a retired station is not locked to it
    Given the postal prefix "013" is assigned to the station "ds-sp" with assignment version 1
    And I retire the node "ds-sp"
    When I assign the postal prefix "013" to the station "ds-rj" with assignment version 2
    Then the command succeeds
    And the postal prefix "013" resolves to the station "ds-rj" in the region "RJ" with assignment version 2

  Scenario: There is no unassign, a prefix cannot be deleted
    Given the postal prefix "013" is assigned to the station "ds-sp" with assignment version 1
    When a DELETE request is sent to "/service-areas/013"
    Then the response status is 405
    And the postal prefix "013" resolves to the station "ds-sp" in the region "SP" with assignment version 1

  Scenario: RegionDefined carries the region, its code and its name
    When I define the region "MG" named "Minas Gerais"
    Then the last command published a "RegionDefined" event whose "Code" is "MG"
    And the last command published a "RegionDefined" event whose "Name" is "Minas Gerais"
    And the last command published a "RegionDefined" event whose "RegionID" is "region:MG"

  Scenario: A refused region code publishes no event and defines nothing
    When I define the region "SP" named "Sao Paulo again"
    Then the command is refused with "region-code-taken"
    And the last command published no events
    And the regions are listed as "SP, RJ"
    And the region "SP" is named "Region SP"

  Scenario Outline: A region cannot be renamed or removed
    When a <method> request is sent to "/regions/region:SP"
    Then the response status is 405
    And the response problem is "method-not-allowed"

    Examples:
      | method |
      | DELETE |
      | PUT    |
      | PATCH  |
