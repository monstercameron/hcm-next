package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func workerIDsPage(view View) ui.Node {
	return ui.CreateElement(WorkerIDPage, WorkerIDPageProps{I18nProps: I18nProps{Locale: view.Locale}, Policy: view.WorkerIDPolicy, Back: ActionLinkProps{Label: "← " + view.Locale.Text("page.admin.title"), Href: statefulHref(view, PageAdmin), Class: "button secondary", Navigate: view.Navigate}, Editable: len(view.EffectivePermissions) == 0 || view.Can(PageWorkerIDs, "update"), OnSave: view.SaveWorkerIDPolicy})
}
