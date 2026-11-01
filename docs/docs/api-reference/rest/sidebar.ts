import type { SidebarsConfig } from "@docusaurus/plugin-content-docs";

const sidebar: SidebarsConfig = {
  apisidebar: [
    {
      type: "doc",
      id: "api-reference/rest/node-topology-api",
    },
    {
      type: "category",
      label: "Nodes",
      link: {
        type: "doc",
        id: "api-reference/rest/nodes",
      },
      items: [
        {
          type: "doc",
          id: "api-reference/rest/register-node",
          label: "Register a node (RegisterNode)",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api-reference/rest/list-nodes",
          label: "List nodes by type, region and status (ListNodes)",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api-reference/rest/get-node",
          label: "Get one node with its current operational profile (GetNode)",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api-reference/rest/declare-operational-profile",
          label: "Declare a new operational profile revision (DeclareOperationalProfile)",
          className: "api-method put",
        },
        {
          type: "doc",
          id: "api-reference/rest/activate-node",
          label: "Activate a node (ActivateNode)",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api-reference/rest/suspend-node",
          label: "Suspend a node (SuspendNode)",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api-reference/rest/retire-node",
          label: "Retire a node (RetireNode)",
          className: "api-method post",
        },
      ],
    },
    {
      type: "category",
      label: "Lanes",
      link: {
        type: "doc",
        id: "api-reference/rest/lanes",
      },
      items: [
        {
          type: "doc",
          id: "api-reference/rest/open-lane",
          label: "Open a lane between two nodes (OpenLane)",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api-reference/rest/list-lanes",
          label: "List lanes by origin and destination (ListLanes)",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api-reference/rest/close-lane",
          label: "Close a lane (CloseLane)",
          className: "api-method post",
        },
      ],
    },
    {
      type: "category",
      label: "Service Areas",
      link: {
        type: "doc",
        id: "api-reference/rest/service-areas",
      },
      items: [
        {
          type: "doc",
          id: "api-reference/rest/assign-service-area",
          label: "Assign a postal prefix to a delivery station (AssignServiceArea)",
          className: "api-method put",
        },
        {
          type: "doc",
          id: "api-reference/rest/resolve-service-area",
          label: "Resolve which delivery station serves a postal prefix (ResolveServiceArea)",
          className: "api-method get",
        },
      ],
    },
    {
      type: "category",
      label: "Regions",
      link: {
        type: "doc",
        id: "api-reference/rest/regions",
      },
      items: [
        {
          type: "doc",
          id: "api-reference/rest/define-region",
          label: "Define a region (DefineRegion)",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api-reference/rest/list-regions",
          label: "List regions (ListRegions)",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api-reference/rest/get-region",
          label: "Get a region (GetRegion)",
          className: "api-method get",
        },
      ],
    },
  ],
};

export default sidebar.apisidebar;
