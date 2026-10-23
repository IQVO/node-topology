Feature: Service areas
  A service area says which ACTIVE delivery station serves a postal prefix.
  The prefix is reassigned under a strictly higher assignment version, and the
  area always sits in the region of its station.

  Background:
    Given the region "SP" is defined
    And the region "RJ" is defined
    And an active "DELIVERY_STATION" node "ds-sp" exists in the region "SP"
    And an active "DELIVERY_STATION" node "ds-rj" exists in the region "RJ"

  Scenario: Assigning a postal prefix to a delivery station
    When I assign the postal prefix "013" to the station "ds-sp" with assignment version 1
    Then the command succeeds
    And the last command published the event "ServiceAreaAssigned"
    And the postal prefix "013" resolves to the station "ds-sp" in the region "SP" with assignment version 1

  Scenario: The station must be ACTIVE
    Given a "DELIVERY_STATION" node "ds-new" exists in the region "SP"
    When I assign the postal prefix "013" to the station "ds-new" with assignment version 1
    Then the command is refused with "not-a-delivery-station"
    And nothing was stored by the last command
    And the postal prefix "013" is not assigned

  Scenario: A suspended station serves nothing new
    Given I suspend the node "ds-sp" because "audit"
    When I assign the postal prefix "013" to the station "ds-sp" with assignment version 1
    Then the command is refused with "not-a-delivery-station"

  Scenario: The station must be a delivery station
    Given an active "SORTATION_CENTER" node "sc-1" exists in the region "SP"
    When I assign the postal prefix "013" to the station "sc-1" with assignment version 1
    Then the command is refused with "not-a-delivery-station"

  Scenario: An unknown station is not found
    When I assign the postal prefix "013" to the node id "node-404" with assignment version 1
    Then the command is refused with "not-found"

  Scenario: Reassigning under a higher version replaces the station and the region
    Given the postal prefix "013" is assigned to the station "ds-sp" with assignment version 1
    When I assign the postal prefix "013" to the station "ds-rj" with assignment version 2
    Then the command succeeds
    And the last command published the event "ServiceAreaAssigned"
    And the postal prefix "013" resolves to the station "ds-rj" in the region "RJ" with assignment version 2

  Scenario: The assignment version strictly increases
    Given the postal prefix "013" is assigned to the station "ds-sp" with assignment version 3
    When I assign the postal prefix "013" to the station "ds-rj" with assignment version 3
    Then the command is refused with "stale-assignment-version"
    And nothing was stored by the last command
    And the postal prefix "013" resolves to the station "ds-sp" in the region "SP" with assignment version 3

  Scenario: An older version is refused too
    Given the postal prefix "013" is assigned to the station "ds-sp" with assignment version 3
    When I assign the postal prefix "013" to the station "ds-rj" with assignment version 2
    Then the command is refused with "stale-assignment-version"

  Scenario: The assignment version starts at one
    When I assign the postal prefix "013" to the station "ds-sp" with assignment version 0
    Then the command is refused with "validation-failed"

  Scenario: Prefixes are matched exactly
    Given the postal prefix "013" is assigned to the station "ds-sp" with assignment version 1
    Then the postal prefix "01" is not assigned
    And the postal prefix "0130" is not assigned
