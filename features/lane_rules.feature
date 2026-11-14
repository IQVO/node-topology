Feature: Lane rules from the documentation
  Derived from the OpenLane and CloseLane use cases and the Lane invariants:
  the allowed type pairs of the lane rules hold in both modes, the checks run
  in the order of the main flow (loop, type pair, endpoint state, duplicate),
  and an OPEN lane is never closed by its end leaving service
  (SuspendedEndpointsUnusable).

  Background:
    Given the region "SP" is defined

  Scenario: LaneOpened carries the ends, the mode and the nominal transit
    Given an active "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    And an active "SORTATION_CENTER" node "sc-1" exists in the region "SP"
    When I open a GROUND lane "L1" from "fc-1" to "sc-1" with a transit of 240 minutes
    Then the command succeeds
    And the last command published a "LaneOpened" event whose "OriginNodeID" is "node:fc-1"
    And the last command published a "LaneOpened" event whose "DestinationNodeID" is "node:sc-1"
    And the last command published a "LaneOpened" event whose "Mode" is "GROUND"
    And the last command published a "LaneOpened" event whose "NominalTransitMinutes" is "240"

  Scenario: LaneClosed carries the lane and the instant it closed
    Given an active "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    And an active "SORTATION_CENTER" node "sc-1" exists in the region "SP"
    And an open GROUND lane "L1" exists from "fc-1" to "sc-1"
    When I close the lane "L1"
    Then the last command published a "LaneClosed" event whose "LaneID" is "lane:L1"
    And the last command published a "LaneClosed" event whose "ClosedAt" is "2026-10-23T12:00:00Z"

  Scenario Outline: The allowed type pairs hold in AIR mode as well
    Given an active "<origin type>" node "o" exists in the region "SP"
    And an active "<destination type>" node "d" exists in the region "SP"
    When I open a AIR lane "L1" from "o" to "d"
    Then the command succeeds
    And the last command published a "LaneOpened" event whose "Mode" is "AIR"

    Examples:
      | origin type        | destination type   |
      | FULFILLMENT_CENTER | AIR_HUB            |
      | AIR_HUB            | AIR_HUB            |
      | AIR_HUB            | DELIVERY_STATION   |
      | SORTATION_CENTER   | AIR_HUB            |

  Scenario: A pair outside the lane rules is refused in AIR mode too
    Given an active "DELIVERY_STATION" node "ds-1" exists in the region "SP"
    And an active "SORTATION_CENTER" node "sc-1" exists in the region "SP"
    When I open a AIR lane "L1" from "ds-1" to "sc-1"
    Then the command is refused with "lane-type-pair-not-allowed"
    And nothing was stored by the last command

  Scenario Outline: Further type pairs the lane rules do not list
    Given an active "<origin type>" node "o" exists in the region "SP"
    And an active "<destination type>" node "d" exists in the region "SP"
    When I open a GROUND lane "L1" from "o" to "d"
    Then the command is refused with "lane-type-pair-not-allowed"
    And the last command published no events

    Examples:
      | origin type        | destination type   |
      | FULFILLMENT_CENTER | FULFILLMENT_CENTER |
      | FULFILLMENT_CENTER | RETURNS_CENTER     |
      | FULFILLMENT_CENTER | INBOUND_CROSS_DOCK |
      | INBOUND_CROSS_DOCK | SORTATION_CENTER   |
      | INBOUND_CROSS_DOCK | DELIVERY_STATION   |
      | INBOUND_CROSS_DOCK | AIR_HUB            |
      | RETURNS_CENTER     | FULFILLMENT_CENTER |
      | SORTATION_CENTER   | RETURNS_CENTER     |
      | DELIVERY_STATION   | AIR_HUB            |

  Scenario: The loop is refused before the state of the node is looked at
    Given a "SORTATION_CENTER" node "sc-1" is PROPOSED in the region "SP"
    When I open a GROUND lane "L1" from "sc-1" to "sc-1"
    Then the command is refused with "lane-loop"

  Scenario: The type pair is refused before the state of the ends is looked at
    Given a "DELIVERY_STATION" node "ds-1" is PROPOSED in the region "SP"
    And an active "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    When I open a GROUND lane "L1" from "ds-1" to "fc-1"
    Then the command is refused with "lane-type-pair-not-allowed"

  Scenario Outline: A node that is not ACTIVE cannot be an end of a new lane
    Given a "SORTATION_CENTER" node "sc-1" is <state> in the region "SP"
    And an active "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    And an active "DELIVERY_STATION" node "ds-1" exists in the region "SP"
    When I open a GROUND lane "L1" from "<origin>" to "<destination>"
    Then the command is refused with "lane-endpoint-not-active"
    And nothing was stored by the last command

    Examples:
      | state     | origin | destination |
      | PROPOSED  | fc-1   | sc-1        |
      | SUSPENDED | fc-1   | sc-1        |
      | RETIRED   | fc-1   | sc-1        |
      | PROPOSED  | sc-1   | ds-1        |
      | SUSPENDED | sc-1   | ds-1        |
      | RETIRED   | sc-1   | ds-1        |

  Scenario: Suspending and reactivating an end does not free the slot of its OPEN lane
    Given an active "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    And an active "DELIVERY_STATION" node "ds-1" exists in the region "SP"
    And an open GROUND lane "L1" exists from "fc-1" to "ds-1"
    And I suspend the node "ds-1" because "flooded dock"
    And I activate the node "ds-1"
    When I open a GROUND lane "L2" from "fc-1" to "ds-1"
    Then the command is refused with "duplicate-lane"
    And the lane "L1" is OPEN

  Scenario: The planner can still close the lane of a retired end
    Given an active "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    And an active "DELIVERY_STATION" node "ds-1" exists in the region "SP"
    And an open GROUND lane "L1" exists from "fc-1" to "ds-1"
    And I retire the node "ds-1"
    When I close the lane "L1"
    Then the command succeeds
    And the lane "L1" is CLOSED

  Scenario: A lane closed by the planner can be opened again as a new lane
    Given an active "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    And an active "DELIVERY_STATION" node "ds-1" exists in the region "SP"
    And an open GROUND lane "L1" exists from "fc-1" to "ds-1"
    And I close the lane "L1"
    When I open a GROUND lane "L2" from "fc-1" to "ds-1"
    Then the command succeeds
    And the lane "L1" is CLOSED
    And the lane "L2" is OPEN
    And the last command published the event "LaneOpened"

  Scenario: The lanes of a node are listed by origin and destination
    Given an active "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    And an active "SORTATION_CENTER" node "sc-1" exists in the region "SP"
    And an active "DELIVERY_STATION" node "ds-1" exists in the region "SP"
    And an open GROUND lane "L1" exists from "fc-1" to "sc-1"
    And an open GROUND lane "L2" exists from "fc-1" to "ds-1"
    When a GET request is sent to "/lanes?origin=node:fc-1&destination=node:ds-1"
    Then the response status is 200
    And the response list "items" has 1 items
    And the response field "items.0.laneId" is "lane:L2"

  Scenario: Listing the lanes into a node leaves out the lanes into other nodes
    Given an active "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    And an active "SORTATION_CENTER" node "sc-1" exists in the region "SP"
    And an active "DELIVERY_STATION" node "ds-1" exists in the region "SP"
    And an open GROUND lane "L1" exists from "fc-1" to "sc-1"
    And an open GROUND lane "L2" exists from "sc-1" to "ds-1"
    When a GET request is sent to "/lanes?destination=node:sc-1"
    Then the response list "items" has 1 items
    And the response field "items.0.laneId" is "lane:L1"
