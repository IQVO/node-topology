Feature: Suspension and retirement
  Suspending a node changes only the node: its lanes stay OPEN and its service
  areas stay assigned. Whatever consumes them treats a suspended end as
  unusable. A suspended node can be reactivated; RETIRED is terminal.

  Background:
    Given the region "SP" is defined
    And an active "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    And an active "DELIVERY_STATION" node "ds-1" exists in the region "SP"
    And an open GROUND lane "L1" exists from "fc-1" to "ds-1"
    And the postal prefix "013" is assigned to the station "ds-1" with assignment version 1

  Scenario: Suspending an end keeps its lanes OPEN
    When I suspend the node "ds-1" because "flooded dock"
    Then the command succeeds
    And the last command published the event "NodeSuspended"
    And the node "ds-1" is suspended because "flooded dock"
    And the lane "L1" is OPEN

  Scenario: Suspending a station keeps its service areas
    When I suspend the node "ds-1" because "flooded dock"
    Then the postal prefix "013" resolves to the station "ds-1" in the region "SP" with assignment version 1

  Scenario: Suspending publishes no lane or service area event
    When I suspend the node "ds-1" because "flooded dock"
    Then the last command published the event "NodeSuspended"

  Scenario: Only an ACTIVE node can be suspended
    Given I suspend the node "ds-1" because "flooded dock"
    When I suspend the node "ds-1" because "again"
    Then the command is refused with "illegal-transition"

  Scenario: A suspension needs a reason
    When I suspend the node "ds-1" because " "
    Then the command is refused with "validation-failed"
    And the node "ds-1" is ACTIVE

  Scenario: A suspended node can be reactivated and its lane was never closed
    Given I suspend the node "fc-1" because "audit"
    When I activate the node "fc-1"
    Then the command succeeds
    And the node "fc-1" is ACTIVE
    And the lane "L1" is OPEN

  Scenario: A suspended node can be retired
    Given I suspend the node "ds-1" because "closing down"
    When I retire the node "ds-1"
    Then the command succeeds
    And the last command published the event "NodeRetired"
    And the node "ds-1" is RETIRED
    And the lane "L1" is OPEN

  Scenario: RETIRED is terminal
    Given I retire the node "ds-1"
    When I activate the node "ds-1"
    Then the command is refused with "node-retired"
    And the node "ds-1" is RETIRED

  Scenario: A PROPOSED node cannot be retired
    Given a "DELIVERY_STATION" node "ds-2" exists in the region "SP"
    When I retire the node "ds-2"
    Then the command is refused with "illegal-transition"
