Feature: Lanes
  A lane is a directed, mode-specific connection between two nodes. Only the
  type pairs of the model's lane rules are allowed, both ends must be ACTIVE
  when it opens, and at most one lane is OPEN per origin, destination and mode.

  Background:
    Given the region "SP" is defined
    And an active "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    And an active "SORTATION_CENTER" node "sc-1" exists in the region "SP"
    And an active "DELIVERY_STATION" node "ds-1" exists in the region "SP"

  Scenario: Opening a lane between an allowed pair
    When I open a GROUND lane "L1" from "fc-1" to "sc-1"
    Then the command succeeds
    And the last command published the event "LaneOpened"
    And the lane "L1" is OPEN

  Scenario Outline: Allowed type pairs
    Given an active "<origin type>" node "o" exists in the region "SP"
    And an active "<destination type>" node "d" exists in the region "SP"
    When I open a GROUND lane "L1" from "o" to "d"
    Then the command succeeds

    Examples:
      | origin type        | destination type   |
      | FULFILLMENT_CENTER | SORTATION_CENTER   |
      | FULFILLMENT_CENTER | DELIVERY_STATION   |
      | FULFILLMENT_CENTER | AIR_HUB            |
      | SORTATION_CENTER   | SORTATION_CENTER   |
      | SORTATION_CENTER   | DELIVERY_STATION   |
      | SORTATION_CENTER   | AIR_HUB            |
      | AIR_HUB            | AIR_HUB            |
      | AIR_HUB            | SORTATION_CENTER   |
      | AIR_HUB            | DELIVERY_STATION   |
      | INBOUND_CROSS_DOCK | FULFILLMENT_CENTER |

  Scenario Outline: Type pairs outside the lane rules
    Given an active "<origin type>" node "o" exists in the region "SP"
    And an active "<destination type>" node "d" exists in the region "SP"
    When I open a GROUND lane "L1" from "o" to "d"
    Then the command is refused with "lane-type-pair-not-allowed"
    And nothing was stored by the last command

    Examples:
      | origin type      | destination type   |
      | DELIVERY_STATION | SORTATION_CENTER   |
      | DELIVERY_STATION | DELIVERY_STATION   |
      | DELIVERY_STATION | FULFILLMENT_CENTER |
      | SORTATION_CENTER | FULFILLMENT_CENTER |
      | AIR_HUB          | FULFILLMENT_CENTER |
      | RETURNS_CENTER   | SORTATION_CENTER   |

  Scenario: A lane does not loop onto its own node
    When I open a GROUND lane "L1" from "sc-1" to "sc-1"
    Then the command is refused with "lane-loop"

  Scenario: Both ends must be ACTIVE
    Given a "SORTATION_CENTER" node "sc-2" exists in the region "SP"
    When I open a GROUND lane "L1" from "fc-1" to "sc-2"
    Then the command is refused with "lane-endpoint-not-active"
    And nothing was stored by the last command

  Scenario: The origin must be ACTIVE too
    Given a "FULFILLMENT_CENTER" node "fc-2" exists in the region "SP"
    When I open a GROUND lane "L1" from "fc-2" to "sc-1"
    Then the command is refused with "lane-endpoint-not-active"

  Scenario: A suspended end cannot be a new lane end
    Given I suspend the node "sc-1" because "audit"
    When I open a GROUND lane "L1" from "fc-1" to "sc-1"
    Then the command is refused with "lane-endpoint-not-active"

  Scenario: An unknown node is not found
    When I open a GROUND lane "L1" from "fc-1" to the node id "node-404"
    Then the command is refused with "not-found"

  Scenario: At most one OPEN lane per origin, destination and mode
    Given an open GROUND lane "L1" exists from "fc-1" to "sc-1"
    When I open a GROUND lane "L2" from "fc-1" to "sc-1"
    Then the command is refused with "duplicate-lane"
    And nothing was stored by the last command

  Scenario: Another mode is another lane
    Given an open GROUND lane "L1" exists from "fc-1" to "sc-1"
    When I open a AIR lane "L2" from "fc-1" to "sc-1"
    Then the command succeeds

  Scenario: The reverse direction is another lane
    Given an active "AIR_HUB" node "ah-1" exists in the region "SP"
    And an active "AIR_HUB" node "ah-2" exists in the region "SP"
    And an open AIR lane "L1" exists from "ah-1" to "ah-2"
    When I open a AIR lane "L2" from "ah-2" to "ah-1"
    Then the command succeeds

  Scenario: A closed lane frees its slot for a new one
    Given an open GROUND lane "L1" exists from "fc-1" to "sc-1"
    And I close the lane "L1"
    When I open a GROUND lane "L2" from "fc-1" to "sc-1"
    Then the command succeeds
    And the lane "L1" is CLOSED
    And the lane "L2" is OPEN
    And listing the lanes from "fc-1" returns "L1:CLOSED, L2:OPEN"

  Scenario: Closing a lane
    Given an open GROUND lane "L1" exists from "fc-1" to "sc-1"
    When I close the lane "L1"
    Then the command succeeds
    And the last command published the event "LaneClosed"
    And the lane "L1" is CLOSED

  Scenario: A closed lane cannot be closed again
    Given an open GROUND lane "L1" exists from "fc-1" to "sc-1"
    And I close the lane "L1"
    When I close the lane "L1"
    Then the command is refused with "illegal-transition"

  Scenario Outline: The nominal transit time is a positive integer
    When I open a GROUND lane "L1" from "fc-1" to "sc-1" with a transit of <minutes> minutes
    Then the command is refused with "validation-failed"

    Examples:
      | minutes |
      | 0       |
      | -5      |
