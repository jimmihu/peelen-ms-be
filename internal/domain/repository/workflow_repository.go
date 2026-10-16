package repository

import (
	"context"
	"time"

	"peelen-ms-be/internal/domain/entity"
)

type WorkflowRepository interface {
	ListSites(context.Context, entity.OperationalAccess) ([]entity.Site, error)
	GetSite(context.Context, entity.OperationalAccess, string) (*entity.Site, error)
	CreateSite(context.Context, entity.Site) (*entity.Site, error)
	UpdateSite(context.Context, string, entity.Site) (*entity.Site, error)
	DeleteSite(context.Context, string) error
	ListVendors(context.Context, entity.OperationalAccess, entity.VendorFilter) ([]entity.Vendor, error)
	GetVendor(context.Context, entity.OperationalAccess, string) (*entity.Vendor, error)
	CreateVendor(context.Context, entity.Vendor, []string) (*entity.Vendor, error)
	UpdateVendor(context.Context, string, entity.Vendor, []string) (*entity.Vendor, error)
	DeleteVendor(context.Context, string) error
	ListShifts(context.Context) ([]entity.Shift, error)
	GetShift(context.Context, string) (*entity.Shift, error)
	CreateShift(context.Context, entity.Shift) (*entity.Shift, error)
	UpdateShift(context.Context, string, entity.Shift) (*entity.Shift, error)
	DeleteShift(context.Context, string) error
	ListMyTasks(context.Context, entity.OperationalAccess, time.Time) ([]entity.MyTask, error)
	ListPipelineReports(context.Context, entity.OperationalAccess, entity.ReportFilter) ([]entity.PipelineReport, int, error)
	GetPipelineReport(context.Context, entity.OperationalAccess, string) (*entity.PipelineReport, error)
	CreatePipelineReport(context.Context, entity.OperationalAccess, entity.PipelineReportInput) (*entity.PipelineReport, error)
	UpdatePipelineReport(context.Context, entity.OperationalAccess, string, entity.PipelineReportInput) (*entity.PipelineReport, error)
	RevisePipelineReport(context.Context, entity.OperationalAccess, string, entity.PipelineReportInput) (*entity.PipelineReport, error)
	ListPipelineReportRevisions(context.Context, string) ([]entity.PipelineReportRevision, error)
	ListInventoryReports(context.Context, entity.OperationalAccess, entity.ReportFilter) ([]entity.InventoryReport, int, error)
	GetInventoryReport(context.Context, entity.OperationalAccess, string) (*entity.InventoryReport, error)
	CreateInventoryReport(context.Context, entity.OperationalAccess, entity.InventoryReportInput) (*entity.InventoryReport, error)
	ReviewInventoryReport(context.Context, entity.OperationalAccess, string, string, *string) (*entity.InventoryReport, error)
	ReportDashboard(context.Context, entity.OperationalAccess, time.Time, string, string) (map[string]any, error)
	DailyReport(context.Context, entity.OperationalAccess, time.Time, string, string) ([]map[string]any, error)
}
