package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// DataTableSortDirection is the WAI-ARIA sort state of one column.
type DataTableSortDirection string

const (
	DataTableUnsorted   DataTableSortDirection = "none"
	DataTableAscending  DataTableSortDirection = "ascending"
	DataTableDescending DataTableSortDirection = "descending"
)

// DataTableProps describes a rectangular data surface without coupling the
// renderer to a domain record. Columns determine order and visibility; rows
// address cells by column ID, so callers may reorder or omit columns without
// rebuilding every row.
type DataTableProps struct {
	Caption   string
	AriaLabel string
	SortLabel string
	Class     string
	Columns   []DataTableColumnProps
	Rows      []DataTableRowProps
}

// DataTableColumnProps configures one visible column. A non-empty Href makes
// the header sortable through progressive enhancement and software routing.
type DataTableColumnProps struct {
	ID       string
	Label    string
	Class    string
	Width    string
	Href     string
	Sort     DataTableSortDirection
	Navigate func(string)
	AlignEnd bool
}

// DataTableRowProps is one addressable row. Cells may arrive in any order;
// DataTable renders them in the configured column order and pads omissions.
type DataTableRowProps struct {
	ID    string
	Class string
	Cells []DataTableCellProps
}

// DataTableCellProps carries arbitrary component content for one column.
type DataTableCellProps struct {
	ColumnID  string
	Class     string
	Text      string
	Children  []ui.Node
	RowHeader bool
}

type dataTableRowRenderProps struct {
	Columns []DataTableColumnProps
	Row     DataTableRowProps
}

// DataTable renders a semantic, keyboard-scrollable table. The wrapper owns
// overflow so sticky headers remain attached to the matrix rather than the
// whole application shell.
func DataTable(props DataTableProps) ui.Node {
	columns := normalizedDataTableColumns(props.Columns)
	headings := make([]ui.Node, 0, len(columns))
	for _, column := range columns {
		headings = append(headings, ui.CreateElement(DataTableColumn, column))
	}
	rows := make([]ui.Node, 0, len(props.Rows))
	for _, row := range props.Rows {
		rows = append(rows, ui.CreateElement(dataTableRow, dataTableRowRenderProps{Columns: columns, Row: row}))
	}
	class := strings.TrimSpace("data-table " + props.Class)
	label := strings.TrimSpace(props.AriaLabel)
	if label == "" {
		label = strings.TrimSpace(props.Caption)
	}
	children := make([]ui.Node, 0, 2)
	if props.SortLabel != "" {
		children = append(children, html.Span(html.Props{Class: "data-table-sort-label people-sort-label"}, ui.Text(props.SortLabel)))
	}
	children = append(children, html.Table(html.Props{Class: class},
		html.Caption(html.Props{Class: "sr-only"}, ui.Text(props.Caption)),
		html.Thead(html.Props{}, html.Tr(html.Props{Class: "data-table-head people-columns"}, headings...)),
		html.Tbody(html.Props{Class: "data-table-body people-rows"}, rows...),
	))
	return html.Div(html.Props{Class: "data-table-scroll", Role: "region", TabIndex: html.TabIndexZero, Aria: map[string]string{"label": label}}, children...)
}

// DataTableColumn renders an accessible sortable or static column header.
func DataTableColumn(column DataTableColumnProps) ui.Node {
	class := strings.TrimSpace("data-table-column " + column.Class)
	if column.AlignEnd {
		class += " align-end"
	}
	props := html.Props{Class: class, Raw: map[string]any{"scope": "col"}}
	if column.Width != "" {
		props.Style = map[string]string{"min-width": column.Width}
	}
	if column.Sort != "" && column.Sort != DataTableUnsorted {
		props.Aria = map[string]string{"sort": string(column.Sort)}
	}
	if column.Href == "" {
		return html.Th(props, ui.Text(column.Label))
	}
	indicator := ""
	linkClass := "data-table-sort people-sort"
	if column.Sort == DataTableAscending {
		indicator, linkClass = " ↑", linkClass+" active"
	} else if column.Sort == DataTableDescending {
		indicator, linkClass = " ↓", linkClass+" active"
	}
	return html.Th(props, softwareLink(column.Navigate, html.Props{Class: linkClass, Title: column.Label}, column.Href, ui.Text(column.Label+indicator)))
}

func dataTableRow(props dataTableRowRenderProps) ui.Node {
	cellsByColumn := make(map[string]DataTableCellProps, len(props.Row.Cells))
	for _, cell := range props.Row.Cells {
		if id := strings.TrimSpace(cell.ColumnID); id != "" {
			cellsByColumn[id] = cell
		}
	}
	cells := make([]ui.Node, 0, len(props.Columns))
	for _, column := range props.Columns {
		cell := cellsByColumn[column.ID]
		cell.ColumnID = column.ID
		cells = append(cells, dataTableCell(column, cell))
	}
	class := strings.TrimSpace("data-table-row " + props.Row.Class)
	return html.Tr(html.Props{Class: class, Data: map[string]string{"row-id": props.Row.ID}}, cells...)
}

func dataTableCell(column DataTableColumnProps, cell DataTableCellProps) ui.Node {
	class := strings.TrimSpace("data-table-cell " + cell.Class)
	if column.AlignEnd {
		class += " align-end"
	}
	props := html.Props{Class: class, Data: map[string]string{"column": column.ID, "label": column.Label}}
	children := cell.Children
	if len(children) == 0 && cell.Text != "" {
		children = []ui.Node{ui.Text(cell.Text)}
	}
	if cell.RowHeader {
		props.Raw = map[string]any{"scope": "row"}
		return html.Th(props, children...)
	}
	return html.Td(props, children...)
}

func normalizedDataTableColumns(columns []DataTableColumnProps) []DataTableColumnProps {
	result := make([]DataTableColumnProps, 0, len(columns))
	seen := make(map[string]struct{}, len(columns))
	for _, column := range columns {
		column.ID = strings.TrimSpace(column.ID)
		if column.ID == "" {
			continue
		}
		if _, exists := seen[column.ID]; exists {
			continue
		}
		seen[column.ID] = struct{}{}
		if column.Sort == "" {
			column.Sort = DataTableUnsorted
		}
		result = append(result, column)
	}
	return result
}
