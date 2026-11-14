Feature: Node lifecycle rules from the documentation
  Derived from the context's aggregate design canvas (State Transitions,
  Enforced Invariants, Corrective Policies) and use cases (ActivateNode,
  SuspendNode, RetireNode). A node is PROPOSED, ACTIVE, SUSPENDED or RETIRED;
  a command outside the transition table is illegal, any command on a RETIRED
  node answers node-retired, and suspending or retiring closes nothing the node
  was an end of (SuspendedEndpointsUnusable).

  Background:
    Given the region "SP" is defined

  Scenario Outline: Every edge of the Node transition table
    Given a "DELIVERY_STATION" node "ds-1" is <from> in the region "SP"
    When I send the command "<command>" to the node "ds-1"
    Then the command succeeds
    And the node "ds-1" is <to>
    And the last command published the event "<event>"

    Examples:
      | from      | command  | to        | event         |
      | PROPOSED  | activate | ACTIVE    | NodeActivated |
      | ACTIVE    | suspend  | SUSPENDED | NodeSuspended |
      | SUSPENDED | activate | ACTIVE    | NodeActivated |
      | ACTIVE    | retire   | RETIRED   | NodeRetired   |
      | SUSPENDED | retire   | RETIRED   | NodeRetired   |

  Scenario Outline: A command outside the transition table is an illegal transition
    Given a "DELIVERY_STATION" node "ds-1" is <from> in the region "SP"
    When I send the command "<command>" to the node "ds-1"
    Then the command is refused with "illegal-transition"
    And the node "ds-1" is <from>
    And nothing was stored by the last command

    Examples:
      | from      | command  |
      | PROPOSED  | suspend  |
      | PROPOSED  | retire   |
      | ACTIVE    | activate |
      | SUSPENDED | suspend  |

  Scenario Outline: RETIRED is terminal, every command answers node-retired
    Given a "DELIVERY_STATION" node "ds-1" is RETIRED in the region "SP"
    When I send the command "<command>" to the node "ds-1"
    Then the command is refused with "node-retired"
    And the node "ds-1" is RETIRED
    And nothing was stored by the last command

    Examples:
      | command  |
      | activate |
      | suspend  |
      | retire   |

  Scenario: Activating records when the node became ACTIVE
    Given a "DELIVERY_STATION" node "ds-1" is PROPOSED in the region "SP"
    When I activate the node "ds-1"
    Then the last command published a "NodeActivated" event whose "NodeID" is "node:ds-1"
    And the last command published a "NodeActivated" event whose "ActivatedAt" is "2026-10-23T12:00:00Z"

  Scenario: Suspending publishes the planner's reason and the instant
    Given a "DELIVERY_STATION" node "ds-1" is ACTIVE in the region "SP"
    When I suspend the node "ds-1" because "flooded dock"
    Then the last command published a "NodeSuspended" event whose "NodeID" is "node:ds-1"
    And the last command published a "NodeSuspended" event whose "Reason" is "flooded dock"
    And the last command published a "NodeSuspended" event whose "SuspendedAt" is "2026-10-23T12:00:00Z"

  Scenario: Retiring publishes the instant of retirement
    Given a "DELIVERY_STATION" node "ds-1" is SUSPENDED in the region "SP"
    When I retire the node "ds-1"
    Then the last command published a "NodeRetired" event whose "NodeID" is "node:ds-1"
    And the last command published a "NodeRetired" event whose "RetiredAt" is "2026-10-23T12:00:00Z"

  Scenario: A suspension is refused when the node is PROPOSED even with a good reason
    Given a "DELIVERY_STATION" node "ds-1" is PROPOSED in the region "SP"
    When I suspend the node "ds-1" because "never opened"
    Then the command is refused with "illegal-transition"
    And the last command published no events

  Scenario: A fulfillment center reactivates only while its current profile is complete
    Given a "FULFILLMENT_CENTER" node "fc-1" is SUSPENDED in the region "SP"
    When I activate the node "fc-1"
    Then the command succeeds
    And the node "fc-1" is ACTIVE
    And the node "fc-1" has profile revision 1

  Scenario: Retiring a station keeps its service area assigned
    Given an active "DELIVERY_STATION" node "ds-1" exists in the region "SP"
    And the postal prefix "013" is assigned to the station "ds-1" with assignment version 1
    When I retire the node "ds-1"
    Then the last command published the event "NodeRetired"
    And the postal prefix "013" resolves to the station "ds-1" in the region "SP" with assignment version 1

  Scenario: Retiring an end keeps its lane OPEN and publishes no lane event
    Given an active "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    And an active "DELIVERY_STATION" node "ds-1" exists in the region "SP"
    And an open GROUND lane "L1" exists from "fc-1" to "ds-1"
    When I retire the node "fc-1"
    Then the last command published the event "NodeRetired"
    And the lane "L1" is OPEN
    And listing the lanes from "fc-1" returns "L1:OPEN"

  Scenario: The planner can still close the lane of a suspended end
    Given an active "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    And an active "DELIVERY_STATION" node "ds-1" exists in the region "SP"
    And an open GROUND lane "L1" exists from "fc-1" to "ds-1"
    And I suspend the node "ds-1" because "flooded dock"
    When I close the lane "L1"
    Then the command succeeds
    And the last command published the event "LaneClosed"
    And the lane "L1" is CLOSED

  Scenario: A suspended station can be reassigned to an active one, the old area is not locked
    Given the region "RJ" is defined
    And an active "DELIVERY_STATION" node "ds-sp" exists in the region "SP"
    And an active "DELIVERY_STATION" node "ds-rj" exists in the region "RJ"
    And the postal prefix "013" is assigned to the station "ds-sp" with assignment version 1
    And I suspend the node "ds-sp" because "audit"
    When I assign the postal prefix "013" to the station "ds-rj" with assignment version 2
    Then the command succeeds
    And the postal prefix "013" resolves to the station "ds-rj" in the region "RJ" with assignment version 2

  Scenario: Retired nodes stay listed
    Given a "DELIVERY_STATION" node "ds-1" is RETIRED in the region "SP"
    And an active "DELIVERY_STATION" node "ds-2" exists in the region "SP"
    Then listing nodes with the type "DELIVERY_STATION", status "RETIRED" and region "" returns "ds-1"
    And listing nodes with the type "DELIVERY_STATION", status "" and region "" returns "ds-1, ds-2"
