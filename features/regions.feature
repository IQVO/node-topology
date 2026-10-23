Feature: Regions
  A region is a largely self-sufficient partition of the network. Its code is
  unique, and regions can be read back by id or listed.

  Scenario: Defining a region publishes RegionDefined
    When I define the region "SP" named "Sao Paulo"
    Then the command succeeds
    And the last command published the event "RegionDefined"
    And the region "SP" is named "Sao Paulo"

  Scenario: A region code is unique
    Given the region "SP" is defined
    When I define the region "SP" named "Another Sao Paulo"
    Then the command is refused with "region-code-taken"
    And nothing was stored by the last command
    And the regions are listed as "SP"

  Scenario: A region code is not a free-form string
    When I define the region "not a code" named "Broken"
    Then the command is refused with "validation-failed"
    And nothing was stored by the last command

  Scenario: Regions are listed in id order
    Given the region "SP" is defined
    And the region "RJ" is defined
    And the region "MG" is defined
    Then the regions are listed as "SP, RJ, MG"

  Scenario: Listing regions when none is defined
    Then the regions list is empty

  Scenario: Reading a region by id
    Given the region "RJ" is defined
    When I read the region "RJ"
    Then the command succeeds

  Scenario: Reading an unknown region
    When I read the region with the id "region-404"
    Then the command is refused with "not-found"
