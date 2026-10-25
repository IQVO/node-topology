Feature: Inbound limits of an operational profile
  A profile revision may declare what the node accepts on the inbound dock:
  the heaviest carton in grams, the largest carton in millimetres, whether it
  receives pallets and whether every carton must carry an SSCC. The limits are
  optional; a revision without them declares none, and consumers then treat
  inbound limits as unchecked. Every limit is a positive integer.

  Background:
    Given the region "SP" is defined
    And a "FULFILLMENT_CENTER" node "fc-1" exists in the region "SP"

  Scenario: Declaring every inbound limit
    When I declare revision 1 of the node "fc-1" with the inbound limits 25000 g, 600 x 400 x 300 mm, pallets accepted and SSCC required
    Then the command succeeds
    And the last command published the event "NodeOperationalProfileDeclared"
    And the published profile carries the inbound limits 25000 g and 600 x 400 x 300 mm
    And the node "fc-1" shows the inbound limits 25000 g and 600 x 400 x 300 mm

  Scenario: A profile without inbound limits declares none
    When I declare revision 1 of the node "fc-1" with 30 minutes of handling and 1 cutoff
    Then the command succeeds
    And the published profile carries no inbound limits
    And the node "fc-1" shows no inbound limits

  Scenario: A later revision without inbound limits drops them, earlier ones are kept
    Given I declare revision 1 of the node "fc-1" with the inbound limits 25000 g, 600 x 400 x 300 mm, pallets accepted and SSCC required
    When I declare revision 2 of the node "fc-1" with 30 minutes of handling and 1 cutoff
    Then the command succeeds
    And the node "fc-1" shows no inbound limits
    And revision 1 of the node "fc-1" still has the inbound limits 25000 g and 600 x 400 x 300 mm

  Scenario Outline: A limit that is not a positive integer is refused
    When I declare revision 1 of the node "fc-1" with the inbound limits <weight> g, <l> x <w> x <h> mm, pallets accepted and SSCC required
    Then the command is refused with "validation-failed"
    And nothing was stored by the last command

    Examples:
      | weight | l   | w   | h   |
      | 0      | 600 | 400 | 300 |
      | -1     | 600 | 400 | 300 |
      | 25000  | 0   | 400 | 300 |
      | 25000  | 600 | 0   | 300 |
      | 25000  | 600 | 400 | 0   |
      | 25000  | 600 | -4  | 300 |

  Scenario: Declaring nothing inside inbound is refused
    When I declare revision 1 of the node "fc-1" with an empty inbound declaration
    Then the command is refused with "validation-failed"
    And nothing was stored by the last command
