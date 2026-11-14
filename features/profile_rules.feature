Feature: Operational profile rules from the documentation
  Derived from the DeclareOperationalProfile use case, the Node invariants of
  the aggregate design canvas and the domain-events page: cutoffs fall inside
  the operating hours of every day they apply to (default: every operating
  day), a sort dwell belongs to sorting nodes, the throughput cap and the
  holidays have a fixed shape, and only an ACTIVE fulfillment center must keep
  a cutoff and a handling time above zero.

  Background:
    Given the region "SP" is defined

  Scenario Outline: A cutoff at the edge of the operating hours (Monday 06:00-22:00)
    Given a "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    When I declare revision 1 of the node "fc-1" with a cutoff at "<at>" on "monday"
    Then the command succeeds
    And the last command published the event "NodeOperationalProfileDeclared"
    And the node "fc-1" has profile revision 1

    Examples:
      | at    |
      | 06:00 |
      | 06:01 |
      | 21:59 |

  Scenario Outline: A cutoff before opening or after closing is refused (Monday 06:00-22:00)
    Given a "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    When I declare revision 1 of the node "fc-1" with a cutoff at "<at>" on "monday"
    Then the command is refused with "cutoff-outside-operating-hours"
    And the last command published no events
    And nothing was stored by the last command
    And the node "fc-1" has no profile

    Examples:
      | at    |
      | 00:00 |
      | 05:59 |
      | 22:01 |

  Scenario: A cutoff on a day the node is closed is refused
    Given a "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    When I declare revision 1 of the node "fc-1" with a cutoff at "11:00" on "sunday"
    Then the command is refused with "cutoff-outside-operating-hours"
    And the node "fc-1" has no profile

  Scenario: A cutoff without days applies to every operating day, Saturday included
    Given a "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    When I declare revision 1 of the node "fc-1" open Monday to Friday 06:00-22:00 and Saturday 08:00-14:00 with a cutoff at "18:00" on every operating day
    Then the command is refused with "cutoff-outside-operating-hours"
    And the node "fc-1" has no profile

  Scenario: The same 18:00 cutoff is accepted once it names Monday to Friday
    Given a "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    When I declare revision 1 of the node "fc-1" open Monday to Friday 06:00-22:00 and Saturday 08:00-14:00 with a cutoff at "18:00" on "monday,tuesday,wednesday,thursday,friday"
    Then the command succeeds
    And the node "fc-1" has profile revision 1

  Scenario: A cutoff without days inside every operating day's hours is accepted
    Given a "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    When I declare revision 1 of the node "fc-1" open Monday to Friday 06:00-22:00 and Saturday 08:00-14:00 with a cutoff at "11:00" on every operating day
    Then the command succeeds

  Scenario: A cutoff without days is refused when it precedes the shortest day's opening
    Given a "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    When I declare revision 1 of the node "fc-1" open Monday to Friday 06:00-22:00 and Saturday 08:00-14:00 with a cutoff at "07:00" on every operating day
    Then the command is refused with "cutoff-outside-operating-hours"

  Scenario: One bad cutoff refuses the whole declaration
    Given a "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    When I declare revision 1 of the node "fc-1" with a cutoff at "11:00" on "monday" and a cutoff at "23:00" on "tuesday"
    Then the command is refused with "cutoff-outside-operating-hours"
    And nothing was stored by the last command
    And the node "fc-1" has no profile

  Scenario: Two cutoffs on different days are both kept
    Given a "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    When I declare revision 1 of the node "fc-1" with a cutoff at "11:00" on "monday" and a cutoff at "18:00" on "tuesday"
    Then the command succeeds
    And the node "fc-1" has profile revision 1

  Scenario: A refused cutoff leaves the previous revision current
    Given a "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    And the node "fc-1" has declared a complete profile
    When I declare revision 2 of the node "fc-1" with a cutoff at "22:30" on "monday"
    Then the command is refused with "cutoff-outside-operating-hours"
    And the node "fc-1" has profile revision 1
    And the node "fc-1" keeps 1 stored profile revisions

  Scenario Outline: Who declared the revision is recorded on the event
    Given a "<type>" node "n-1" exists in the region "SP"
    When I declare revision 1 of the node "n-1" as declared by "<declarer>"
    Then the command succeeds
    And the last command published a "NodeOperationalProfileDeclared" event whose "DeclaredBy" is "<declarer>"
    And the last command published a "NodeOperationalProfileDeclared" event whose "NodeID" is "node:n-1"
    And the last command published a "NodeOperationalProfileDeclared" event whose "ProfileRevision" is "1"

    Examples:
      | type               | declarer    |
      | FULFILLMENT_CENTER | VENDOR      |
      | FULFILLMENT_CENTER | NETWORK_OPS |
      | DELIVERY_STATION   | NETWORK_OPS |

  Scenario Outline: Only sortation centers and air hubs declare a sort dwell
    Given a "<type>" node "n-1" exists in the region "SP"
    When I declare revision 1 of the node "n-1" with a sort dwell of 20 minutes
    Then the command succeeds
    And the last command published a "NodeOperationalProfileDeclared" event whose "SortDwellMinutes" is "20"

    Examples:
      | type             |
      | SORTATION_CENTER |
      | AIR_HUB          |

  Scenario Outline: Every other node type refuses a sort dwell
    Given a "<type>" node "n-1" exists in the region "SP"
    When I declare revision 1 of the node "n-1" with a sort dwell of 20 minutes
    Then the command is refused with "sort-dwell-not-allowed"
    And the last command published no events
    And the node "n-1" has no profile

    Examples:
      | type               |
      | DELIVERY_STATION   |
      | INBOUND_CROSS_DOCK |
      | RETURNS_CENTER     |

  Scenario: A sort dwell of zero minutes is still a sort dwell
    Given a "DELIVERY_STATION" node "ds-1" exists in the region "SP"
    When I declare revision 1 of the node "ds-1" with a sort dwell of 0 minutes
    Then the command is refused with "sort-dwell-not-allowed"

  Scenario Outline: The throughput cap is a positive number of orders per hour or per day
    Given a "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    When I declare revision 1 of the node "fc-1" with a throughput cap of <value> <unit> per <per>
    Then the command succeeds
    And the node "fc-1" has profile revision 1

    Examples:
      | value | unit  | per  |
      | 1     | ORDER | HOUR |
      | 400   | ORDER | HOUR |
      | 1200  | ORDER | DAY  |

  Scenario Outline: A throughput cap outside that shape is refused
    Given a "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    When I declare revision 1 of the node "fc-1" with a throughput cap of <value> <unit> per <per>
    Then the command is refused with "validation-failed"
    And the node "fc-1" has no profile

    Examples:
      | value | unit   | per    |
      | 0     | ORDER  | HOUR   |
      | -5    | ORDER  | DAY    |
      | 100   | ORDER  | WEEK   |
      | 100   | ORDER  | MINUTE |
      | 100   | PALLET | HOUR   |

  Scenario: A holiday is an ISO date
    Given a "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    When I declare revision 1 of the node "fc-1" with the holiday "2026-12-25"
    Then the command succeeds

  Scenario Outline: A holiday that is not an ISO date is refused
    Given a "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    When I declare revision 1 of the node "fc-1" with the holiday "<date>"
    Then the command is refused with "validation-failed"
    And the node "fc-1" has no profile

    Examples:
      | date       |
      | 25/12/2026 |
      | 2026-02-30 |
      | Christmas  |

  Scenario Outline: The time zone is an IANA name
    Given a "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    When I declare revision 1 of the node "fc-1" in the time zone "<zone>"
    Then the command succeeds

    Examples:
      | zone                           |
      | America/Sao_Paulo              |
      | America/Argentina/Buenos_Aires |
      | UTC                            |

  Scenario Outline: A time zone that is not an IANA name is refused
    Given a "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"
    When I declare revision 1 of the node "fc-1" in the time zone "<zone>"
    Then the command is refused with "validation-failed"
    And the node "fc-1" has no profile

    Examples:
      | zone      |
      | Sao Paulo |
      | 12/34     |

  Scenario: An ACTIVE fulfillment center keeps a handling time above zero
    Given a "FULFILLMENT_CENTER" node "fc-1" is ACTIVE in the region "SP"
    When I declare revision 2 of the node "fc-1" with 0 minutes of handling and 1 cutoff
    Then the command is refused with "incomplete-operational-profile"
    And the node "fc-1" has profile revision 1
    And the last command published no events

  Scenario: One minute of handling is above zero
    Given a "FULFILLMENT_CENTER" node "fc-1" is ACTIVE in the region "SP"
    When I declare revision 2 of the node "fc-1" with 1 minutes of handling and 1 cutoff
    Then the command succeeds
    And the node "fc-1" has profile revision 2
    And the node "fc-1" is ACTIVE

  Scenario: A SUSPENDED fulfillment center may declare an incomplete revision but cannot reactivate on it
    Given a "FULFILLMENT_CENTER" node "fc-1" is SUSPENDED in the region "SP"
    And I declare revision 2 of the node "fc-1" with 30 minutes of handling and 0 cutoffs
    When I activate the node "fc-1"
    Then the command is refused with "incomplete-operational-profile"
    And the node "fc-1" is SUSPENDED

  Scenario: A complete revision declared while SUSPENDED lets the fulfillment center reactivate
    Given a "FULFILLMENT_CENTER" node "fc-1" is SUSPENDED in the region "SP"
    And I declare revision 2 of the node "fc-1" with 30 minutes of handling and 0 cutoffs
    And I declare revision 3 of the node "fc-1" with 45 minutes of handling and 1 cutoff
    When I activate the node "fc-1"
    Then the command succeeds
    And the node "fc-1" has profile revision 3

  Scenario: Only a fulfillment center needs a cutoff and a handling time, a delivery station does not
    Given a "DELIVERY_STATION" node "ds-1" is ACTIVE in the region "SP"
    When I declare revision 1 of the node "ds-1" with 0 minutes of handling and 0 cutoffs
    Then the command succeeds
    And the node "ds-1" is ACTIVE

  Scenario: Declaring a profile on an ACTIVE node changes no state
    Given a "FULFILLMENT_CENTER" node "fc-1" is ACTIVE in the region "SP"
    When I declare revision 2 of the node "fc-1" with 45 minutes of handling and 1 cutoff
    Then the command succeeds
    And the last command published the event "NodeOperationalProfileDeclared"
    And the node "fc-1" is ACTIVE
    And the node "fc-1" keeps 2 stored profile revisions
