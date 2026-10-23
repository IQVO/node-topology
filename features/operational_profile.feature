Feature: Operational profile declaration and activation
  A profile is declared as a numbered revision, strictly increasing; every
  revision is kept. A fulfillment center needs a cutoff and a handling time
  above zero to become ACTIVE and to stay ACTIVE.

  Background:
    Given the region "SP" is defined
    And a "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"

  Scenario: Declaring the first revision
    When I declare revision 1 of the node "fc-1" with 30 minutes of handling and 1 cutoff
    Then the command succeeds
    And the last command published the event "NodeOperationalProfileDeclared"
    And the node "fc-1" has profile revision 1
    And the node "fc-1" is PROPOSED

  Scenario: A revision strictly increases
    Given the node "fc-1" has declared a complete profile
    When I declare revision 1 of the node "fc-1" with 45 minutes of handling and 1 cutoff
    Then the command is refused with "stale-profile-revision"
    And nothing was stored by the last command
    And the node "fc-1" has profile revision 1

  Scenario: An older revision is refused as well
    Given the node "fc-1" has declared a complete profile
    And I declare revision 5 of the node "fc-1" with 40 minutes of handling and 1 cutoff
    When I declare revision 3 of the node "fc-1" with 45 minutes of handling and 1 cutoff
    Then the command is refused with "stale-profile-revision"
    And the node "fc-1" has profile revision 5

  Scenario: Revisions may skip numbers
    Given the node "fc-1" has declared a complete profile
    When I declare revision 7 of the node "fc-1" with 45 minutes of handling and 1 cutoff
    Then the command succeeds
    And the node "fc-1" has profile revision 7

  Scenario: Earlier revisions are kept untouched
    Given the node "fc-1" has declared a complete profile
    When I declare revision 2 of the node "fc-1" with 45 minutes of handling and 1 cutoff
    Then the node "fc-1" keeps 2 stored profile revisions
    And revision 1 of the node "fc-1" still has 30 minutes of handling
    And revision 2 of the node "fc-1" still has 45 minutes of handling

  Scenario: An incomplete fulfillment center cannot activate without a profile
    When I activate the node "fc-1"
    Then the command is refused with "incomplete-operational-profile"
    And the node "fc-1" is PROPOSED
    And nothing was stored by the last command

  Scenario: An incomplete fulfillment center cannot activate without a cutoff
    Given I declare revision 1 of the node "fc-1" with 30 minutes of handling and 0 cutoffs
    When I activate the node "fc-1"
    Then the command is refused with "incomplete-operational-profile"
    And the node "fc-1" is PROPOSED

  Scenario: An incomplete fulfillment center cannot activate without handling time
    Given I declare revision 1 of the node "fc-1" with 0 minutes of handling and 1 cutoff
    When I activate the node "fc-1"
    Then the command is refused with "incomplete-operational-profile"
    And the node "fc-1" is PROPOSED

  Scenario: A complete fulfillment center activates
    Given the node "fc-1" has declared a complete profile
    When I activate the node "fc-1"
    Then the command succeeds
    And the last command published the event "NodeActivated"
    And the node "fc-1" is ACTIVE

  Scenario: An active fulfillment center keeps its cutoff
    Given the node "fc-1" has declared a complete profile
    And I activate the node "fc-1"
    When I declare revision 2 of the node "fc-1" with 30 minutes of handling and 0 cutoffs
    Then the command is refused with "incomplete-operational-profile"
    And the node "fc-1" has profile revision 1

  Scenario: A profile can be declared in every live state
    Given the node "fc-1" has declared a complete profile
    And I activate the node "fc-1"
    And I suspend the node "fc-1" because "audit"
    When I declare revision 2 of the node "fc-1" with 45 minutes of handling and 1 cutoff
    Then the command succeeds
    And the node "fc-1" is SUSPENDED

  Scenario: A cutoff must fall inside the operating hours
    When I declare revision 1 of the node "fc-1" with a Monday cutoff at "23:30"
    Then the command is refused with "cutoff-outside-operating-hours"
    And nothing was stored by the last command

  Scenario: A sort dwell belongs to sorting nodes only
    When I declare revision 1 of the node "fc-1" with a sort dwell of 20 minutes
    Then the command is refused with "sort-dwell-not-allowed"

  Scenario: A retired node takes no new profile
    Given the node "fc-1" has declared a complete profile
    And I activate the node "fc-1"
    And I retire the node "fc-1"
    When I declare revision 2 of the node "fc-1" with 45 minutes of handling and 1 cutoff
    Then the command is refused with "node-retired"

  Scenario: A delivery station activates without a profile
    Given a "DELIVERY_STATION" node "ds-1" exists in the region "SP"
    When I activate the node "ds-1"
    Then the command succeeds
    And the node "ds-1" is ACTIVE
