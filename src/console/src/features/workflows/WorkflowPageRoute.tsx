import { findInitialPageDefinition } from "@hcm-next/ui-runtime";
import { useQuery } from "@tanstack/react-query";
import { WorkflowPageRenderer } from "../../runtime/WorkflowPageRenderer";
import { demoRuntimeContext } from "../../app/demo-data";
import type {
  FieldControlBaseStyleProps,
  WidgetStyleProps,
} from "../../runtime/control-library";

type WorkflowPageRouteProps = {
  fieldBrandingStyleProps?: FieldControlBaseStyleProps;
  pageId: string;
  widgetBrandingStyleProps?: WidgetStyleProps;
};

export function WorkflowPageRoute({
  fieldBrandingStyleProps,
  pageId,
  widgetBrandingStyleProps,
}: WorkflowPageRouteProps): JSX.Element {
  const pageQuery = useQuery({
    queryKey: ["page-definition", pageId],
    queryFn: () => Promise.resolve(findInitialPageDefinition(pageId)),
  });

  if (pageQuery.isLoading) {
    return <div className="loading-state">Loading page definition...</div>;
  }

  if (pageQuery.data === undefined) {
    return (
      <div className="empty-state">
        No page definition is registered for <code>{pageId}</code>.
      </div>
    );
  }

  return (
    <WorkflowPageRenderer
      {...(fieldBrandingStyleProps === undefined ? {} : { fieldBrandingStyleProps })}
      page={pageQuery.data}
      runtimeContext={demoRuntimeContext}
      {...(widgetBrandingStyleProps === undefined ? {} : { widgetBrandingStyleProps })}
    />
  );
}
