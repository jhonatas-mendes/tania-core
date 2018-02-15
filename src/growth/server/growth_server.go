package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/Tanibox/tania-server/config"
	"github.com/Tanibox/tania-server/src/growth/domain"
	"github.com/Tanibox/tania-server/src/growth/domain/service"
	"github.com/Tanibox/tania-server/src/growth/query/inmemory"
	"github.com/Tanibox/tania-server/src/helper/imagehelper"
	"github.com/Tanibox/tania-server/src/helper/stringhelper"
	"github.com/Tanibox/tania-server/src/helper/structhelper"

	assetsstorage "github.com/Tanibox/tania-server/src/assets/storage"
	"github.com/Tanibox/tania-server/src/growth/query"
	"github.com/Tanibox/tania-server/src/growth/repository"
	"github.com/Tanibox/tania-server/src/growth/storage"
	"github.com/asaskevich/EventBus"
	"github.com/labstack/echo"
	uuid "github.com/satori/go.uuid"
)

// GrowthServer ties the routes and handlers with injected dependencies
type GrowthServer struct {
	CropEventRepo     repository.CropEventRepository
	CropEventQuery    query.CropEventQuery
	CropReadRepo      repository.CropReadRepository
	CropReadQuery     query.CropReadQuery
	CropActivityRepo  repository.CropActivityRepository
	CropActivityQuery query.CropActivityQuery
	CropService       domain.CropService
	AreaReadQuery     query.AreaReadQuery
	MaterialReadQuery query.MaterialReadQuery
	FarmReadQuery     query.FarmReadQuery
	EventBus          EventBus.Bus
	File              File
}

// NewGrowthServer initializes GrowthServer's dependencies and create new GrowthServer struct
func NewGrowthServer(
	bus EventBus.Bus,
	cropEventStorage *storage.CropEventStorage,
	cropReadStorage *storage.CropReadStorage,
	cropActivityStorage *storage.CropActivityStorage,
	areaReadStorage *assetsstorage.AreaReadStorage,
	materialReadStorage *assetsstorage.MaterialReadStorage,
	farmReadStorage *assetsstorage.FarmReadStorage,
) (*GrowthServer, error) {
	cropEventRepo := repository.NewCropEventRepositoryInMemory(cropEventStorage)
	cropEventQuery := inmemory.NewCropEventQueryInMemory(cropEventStorage)
	cropReadRepo := repository.NewCropReadRepositoryInMemory(cropReadStorage)
	cropReadQuery := inmemory.NewCropReadQueryInMemory(cropReadStorage)
	cropActivityRepo := repository.NewCropActivityRepositoryInMemory(cropActivityStorage)
	cropActivityQuery := inmemory.NewCropActivityQueryInMemory(cropActivityStorage)

	areaReadQuery := inmemory.NewAreaReadQueryInMemory(areaReadStorage)
	materialReadQuery := inmemory.NewMaterialReadQueryInMemory(materialReadStorage)
	farmReadQuery := inmemory.NewFarmReadQueryInMemory(farmReadStorage)

	cropService := service.CropServiceInMemory{
		MaterialReadQuery: materialReadQuery,
		CropReadQuery:     cropReadQuery,
		AreaReadQuery:     areaReadQuery,
	}

	growthServer := &GrowthServer{
		CropEventRepo:     cropEventRepo,
		CropEventQuery:    cropEventQuery,
		CropReadRepo:      cropReadRepo,
		CropReadQuery:     cropReadQuery,
		CropActivityRepo:  cropActivityRepo,
		CropActivityQuery: cropActivityQuery,
		CropService:       cropService,
		AreaReadQuery:     areaReadQuery,
		MaterialReadQuery: materialReadQuery,
		FarmReadQuery:     farmReadQuery,
		File:              LocalFile{},
		EventBus:          bus,
	}

	growthServer.InitSubscriber()

	return growthServer, nil
}

// InitSubscriber defines the mapping of which event this domain listen with their handler
func (s *GrowthServer) InitSubscriber() {
	s.EventBus.Subscribe("CropBatchCreated", s.SaveToCropReadModel)
	s.EventBus.Subscribe("CropBatchCreated", s.SaveToCropActivityReadModel)
	s.EventBus.Subscribe("CropBatchMoved", s.SaveToCropReadModel)
	s.EventBus.Subscribe("CropBatchMoved", s.SaveToCropActivityReadModel)
	s.EventBus.Subscribe("CropBatchHarvested", s.SaveToCropReadModel)
	s.EventBus.Subscribe("CropBatchHarvested", s.SaveToCropActivityReadModel)
	s.EventBus.Subscribe("CropBatchDumped", s.SaveToCropReadModel)
	s.EventBus.Subscribe("CropBatchDumped", s.SaveToCropActivityReadModel)
	s.EventBus.Subscribe("CropBatchWatered", s.SaveToCropReadModel)
	s.EventBus.Subscribe("CropBatchWatered", s.SaveToCropActivityReadModel)
	s.EventBus.Subscribe("CropBatchNoteCreated", s.SaveToCropReadModel)
	s.EventBus.Subscribe("CropBatchNoteRemoved", s.SaveToCropReadModel)
	s.EventBus.Subscribe("CropBatchPhotoCreated", s.SaveToCropReadModel)
	s.EventBus.Subscribe("CropBatchPhotoCreated", s.SaveToCropActivityReadModel)
}

// Mount defines the GrowthServer's endpoints with its handlers
func (s *GrowthServer) Mount(g *echo.Group) {
	g.GET("/:id/crops", s.FindAllCrops)
	g.GET("/areas/:id/crops", s.FindAllCropsByArea)
	g.POST("/areas/:id/crops", s.SaveAreaCropBatch)
	g.GET("/crops/:id", s.FindCropByID)
	g.POST("/crops/:id/move", s.MoveCrop)
	g.POST("/crops/:id/harvest", s.HarvestCrop)
	g.POST("/crops/:id/dump", s.DumpCrop)
	g.POST("/crops/:id/water", s.WaterCrop)
	g.POST("/crops/:id/notes", s.SaveCropNotes)
	g.DELETE("/crops/:crop_id/notes/:note_id", s.RemoveCropNotes)
	g.POST("/crops/:id/photos", s.UploadCropPhotos)
	g.GET("/crops/:crop_id/photos/:photo_id", s.GetCropPhotos)
	g.GET("/crops/:id/activities", s.GetCropActivities)
	g.GET("/crops/information", s.GetCropsInformation)

}

func (s *GrowthServer) SaveAreaCropBatch(c echo.Context) error {
	// Form Value
	areaID := c.Param("id")
	cropType := c.FormValue("crop_type")
	plantType := c.FormValue("plant_type")
	name := c.FormValue("name")

	containerQuantity, err := strconv.Atoi(c.FormValue("container_quantity"))
	if err != nil {
		return Error(c, err)
	}

	containerType := c.FormValue("container_type")
	containerCell, err := strconv.Atoi(c.FormValue("container_cell"))
	if err != nil {
		return Error(c, err)
	}

	// Validate //
	areaUID, err := uuid.FromString(areaID)
	if err != nil {
		return Error(c, NewRequestValidationError(NOT_FOUND, "id"))
	}

	areaResult := <-s.AreaReadQuery.FindByID(areaUID)
	if areaResult.Error != nil {
		return Error(c, areaResult.Error)
	}

	area, ok := areaResult.Result.(query.CropAreaQueryResult)
	if !ok {
		return Error(c, echo.NewHTTPError(http.StatusBadRequest, "Internal server error"))
	}

	queryResult := <-s.MaterialReadQuery.FindMaterialByPlantTypeCodeAndName(plantType, name)
	if queryResult.Error != nil {
		return Error(c, queryResult.Error)
	}

	material, ok := queryResult.Result.(query.CropMaterialQueryResult)
	if !ok {
		return Error(c, echo.NewHTTPError(http.StatusBadRequest, "Internal server error"))
	}

	var containerT domain.CropContainerType
	switch containerType {
	case domain.Tray{}.Code():
		containerT = domain.Tray{Cell: containerCell}
	case domain.Pot{}.Code():
		containerT = domain.Pot{}
	default:
		return Error(c, NewRequestValidationError(NOT_FOUND, "container_type"))
	}

	// Process //
	cropBatch, err := domain.CreateCropBatch(
		s.CropService,
		area.UID,
		cropType,
		material.UID,
		containerQuantity,
		containerT,
	)
	if err != nil {
		return Error(c, err)
	}

	// Persists //
	err = <-s.CropEventRepo.Save(cropBatch.UID, 0, cropBatch.UncommittedChanges)
	if err != nil {
		return Error(c, err)
	}

	// Trigger Events
	s.publishUncommittedEvents(cropBatch)

	data := make(map[string]storage.CropRead)
	cr, err := MapToCropRead(s, *cropBatch)
	if err != nil {
		return Error(c, err)
	}

	data["data"] = cr

	return c.JSON(http.StatusOK, data)
}

func (s *GrowthServer) FindCropByID(c echo.Context) error {
	cropUID, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return Error(c, err)
	}

	// Validate //
	result := <-s.CropReadQuery.FindByID(cropUID)
	if result.Error != nil {
		return Error(c, result.Error)
	}

	crop, ok := result.Result.(storage.CropRead)
	if !ok {
		return Error(c, echo.NewHTTPError(http.StatusInternalServerError, "Internal server error"))
	}

	if crop.UID == (uuid.UUID{}) {
		return Error(c, NewRequestValidationError(NOT_FOUND, "id"))
	}

	data := make(map[string]storage.CropRead)
	data["data"] = crop

	return c.JSON(http.StatusOK, data)
}

func (s *GrowthServer) MoveCrop(c echo.Context) error {
	cropUID, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return Error(c, err)
	}

	srcAreaID := c.FormValue("source_area_id")
	dstAreaID := c.FormValue("destination_area_id")
	quantity := c.FormValue("quantity")

	// VALIDATE //
	result := <-s.CropReadQuery.FindByID(cropUID)
	if result.Error != nil {
		return Error(c, result.Error)
	}

	cropRead, ok := result.Result.(storage.CropRead)
	if !ok {
		return Error(c, echo.NewHTTPError(http.StatusBadRequest, "Internal server error"))
	}

	if cropRead.UID == (uuid.UUID{}) {
		return Error(c, NewRequestValidationError(NOT_FOUND, "id"))
	}

	srcAreaUID, err := uuid.FromString(srcAreaID)
	if err != nil {
		return Error(c, err)
	}

	dstAreaUID, err := uuid.FromString(dstAreaID)
	if err != nil {
		return Error(c, err)
	}

	qty, err := strconv.Atoi(quantity)
	if err != nil {
		return Error(c, err)
	}

	// PROCESS //
	eventQueryResult := <-s.CropEventQuery.FindAllByCropID(cropUID)
	if eventQueryResult.Error != nil {
		return Error(c, eventQueryResult.Error)
	}

	events := eventQueryResult.Result.([]storage.CropEvent)

	crop := repository.NewCropBatchFromHistory(events)

	err = crop.MoveToArea(s.CropService, srcAreaUID, dstAreaUID, qty)
	if err != nil {
		return Error(c, err)
	}

	// PERSIST //
	err = <-s.CropEventRepo.Save(crop.UID, crop.Version, crop.UncommittedChanges)
	if err != nil {
		return Error(c, err)
	}

	// TRIGGER EVENTS
	s.publishUncommittedEvents(crop)

	data := make(map[string]storage.CropRead)
	cr, err := MapToCropRead(s, *crop)
	if err != nil {
		return Error(c, err)
	}

	data["data"] = cr

	return c.JSON(http.StatusOK, data)
}

func (s *GrowthServer) HarvestCrop(c echo.Context) error {
	cropUID, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return Error(c, err)
	}

	srcAreaID := c.FormValue("source_area_id")
	harvestType := c.FormValue("harvest_type")
	producedQuantity := c.FormValue("produced_quantity")
	producedUnit := c.FormValue("produced_unit")
	notes := c.FormValue("notes")

	// VALIDATE //
	result := <-s.CropReadQuery.FindByID(cropUID)
	if result.Error != nil {
		return Error(c, result.Error)
	}

	cropRead, ok := result.Result.(storage.CropRead)
	if !ok {
		return Error(c, echo.NewHTTPError(http.StatusBadRequest, "Internal server error"))
	}

	if cropRead.UID == (uuid.UUID{}) {
		return Error(c, NewRequestValidationError(NOT_FOUND, "id"))
	}

	srcAreaUID, err := uuid.FromString(srcAreaID)
	if err != nil {
		return Error(c, err)
	}

	ht := domain.GetHarvestType(harvestType)
	if ht == (domain.HarvestType{}) {
		return Error(c, NewRequestValidationError(INVALID_OPTION, "harvest_type"))
	}

	if producedQuantity == "" {
		return Error(c, NewRequestValidationError(REQUIRED, "produced_quantity"))
	}

	prodQty, err := strconv.ParseFloat(producedQuantity, 32)
	if err != nil {
		return Error(c, err)
	}

	prodUnit := domain.GetProducedUnit(producedUnit)
	if prodUnit == (domain.ProducedUnit{}) {
		return Error(c, NewRequestValidationError(INVALID_OPTION, "produced_unit"))
	}

	// PROCESS //
	eventQueryResult := <-s.CropEventQuery.FindAllByCropID(cropUID)
	if eventQueryResult.Error != nil {
		return Error(c, eventQueryResult.Error)
	}

	events := eventQueryResult.Result.([]storage.CropEvent)

	crop := repository.NewCropBatchFromHistory(events)

	err = crop.Harvest(s.CropService, srcAreaUID, harvestType, float32(prodQty), prodUnit, notes)
	if err != nil {
		return Error(c, err)
	}

	// PERSIST //
	err = <-s.CropEventRepo.Save(crop.UID, crop.Version, crop.UncommittedChanges)
	if err != nil {
		return Error(c, err)
	}

	// TRIGGER EVENTS
	s.publishUncommittedEvents(crop)

	data := make(map[string]storage.CropRead)
	cr, err := MapToCropRead(s, *crop)
	if err != nil {
		return Error(c, err)
	}

	data["data"] = cr

	return c.JSON(http.StatusOK, data)
}

func (s *GrowthServer) DumpCrop(c echo.Context) error {
	cropUID, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return Error(c, err)
	}

	srcAreaID := c.FormValue("source_area_id")
	quantity := c.FormValue("quantity")
	notes := c.FormValue("notes")

	// VALIDATE //
	result := <-s.CropReadQuery.FindByID(cropUID)
	if result.Error != nil {
		return Error(c, result.Error)
	}

	cropRead, ok := result.Result.(storage.CropRead)
	if !ok {
		return Error(c, echo.NewHTTPError(http.StatusBadRequest, "Internal server error"))
	}

	if cropRead.UID == (uuid.UUID{}) {
		return Error(c, NewRequestValidationError(NOT_FOUND, "id"))
	}

	srcAreaUID, err := uuid.FromString(srcAreaID)
	if err != nil {
		return Error(c, err)
	}

	qty, err := strconv.Atoi(quantity)
	if err != nil {
		return Error(c, err)
	}

	// PROCESS //
	eventQueryResult := <-s.CropEventQuery.FindAllByCropID(cropUID)
	if eventQueryResult.Error != nil {
		return Error(c, eventQueryResult.Error)
	}

	events := eventQueryResult.Result.([]storage.CropEvent)

	crop := repository.NewCropBatchFromHistory(events)

	err = crop.Dump(s.CropService, srcAreaUID, qty, notes)
	if err != nil {
		return Error(c, err)
	}

	// PERSIST //
	err = <-s.CropEventRepo.Save(crop.UID, crop.Version, crop.UncommittedChanges)
	if err != nil {
		return Error(c, err)
	}

	// TRIGGER EVENTS
	s.publishUncommittedEvents(crop)

	data := make(map[string]storage.CropRead)
	cr, err := MapToCropRead(s, *crop)
	if err != nil {
		return Error(c, err)
	}

	data["data"] = cr

	return c.JSON(http.StatusOK, data)
}

func (s *GrowthServer) WaterCrop(c echo.Context) error {
	cropUID, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return Error(c, err)
	}

	srcAreaID := c.FormValue("source_area_id")
	wateringDate := c.FormValue("watering_date")

	// VALIDATE //
	result := <-s.CropReadQuery.FindByID(cropUID)
	if result.Error != nil {
		return Error(c, result.Error)
	}

	cropRead, ok := result.Result.(storage.CropRead)
	if !ok {
		return Error(c, echo.NewHTTPError(http.StatusBadRequest, "Internal server error"))
	}

	if cropRead.UID == (uuid.UUID{}) {
		return Error(c, NewRequestValidationError(NOT_FOUND, "id"))
	}

	srcAreaUID, err := uuid.FromString(srcAreaID)
	if err != nil {
		return Error(c, err)
	}

	wDate, err := time.Parse("2006-01-02 15:04", wateringDate)
	if err != nil {
		return Error(c, NewRequestValidationError(PARSE_FAILED, "watering_date"))
	}

	// PROCESS //
	eventQueryResult := <-s.CropEventQuery.FindAllByCropID(cropUID)
	if eventQueryResult.Error != nil {
		return Error(c, eventQueryResult.Error)
	}

	events := eventQueryResult.Result.([]storage.CropEvent)

	crop := repository.NewCropBatchFromHistory(events)

	err = crop.Water(s.CropService, srcAreaUID, wDate)
	if err != nil {
		return Error(c, err)
	}

	// PERSIST //
	err = <-s.CropEventRepo.Save(crop.UID, crop.Version, crop.UncommittedChanges)
	if err != nil {
		return Error(c, err)
	}

	// TRIGGER EVENTS //
	s.publishUncommittedEvents(crop)

	data := make(map[string]storage.CropRead)
	cr, err := MapToCropRead(s, *crop)
	if err != nil {
		return Error(c, err)
	}

	data["data"] = cr

	return c.JSON(http.StatusOK, data)
}

func (s *GrowthServer) SaveCropNotes(c echo.Context) error {
	cropUID, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return Error(c, err)
	}

	content := c.FormValue("content")

	// Validate //
	result := <-s.CropReadQuery.FindByID(cropUID)
	if result.Error != nil {
		return Error(c, result.Error)
	}

	cropRead, ok := result.Result.(storage.CropRead)
	if !ok {
		return Error(c, echo.NewHTTPError(http.StatusInternalServerError, "Internal server error"))
	}

	if cropRead.UID == (uuid.UUID{}) {
		return Error(c, NewRequestValidationError(NOT_FOUND, "id"))
	}

	if content == "" {
		return Error(c, NewRequestValidationError(REQUIRED, "content"))
	}

	// Process //
	eventQueryResult := <-s.CropEventQuery.FindAllByCropID(cropUID)
	if eventQueryResult.Error != nil {
		return Error(c, eventQueryResult.Error)
	}

	events := eventQueryResult.Result.([]storage.CropEvent)

	crop := repository.NewCropBatchFromHistory(events)

	err = crop.AddNewNote(content)
	if err != nil {
		return Error(c, err)
	}

	// Persists //
	resultSave := <-s.CropEventRepo.Save(crop.UID, crop.Version, crop.UncommittedChanges)
	if resultSave != nil {
		return Error(c, echo.NewHTTPError(http.StatusInternalServerError, "Internal server error"))
	}

	// TRIGGER EVENTS //
	s.publishUncommittedEvents(crop)

	data := make(map[string]storage.CropRead)
	cr, err := MapToCropRead(s, *crop)
	if err != nil {
		return Error(c, err)
	}

	data["data"] = cr

	return c.JSON(http.StatusOK, data)
}

func (s *GrowthServer) RemoveCropNotes(c echo.Context) error {
	cropUID, err := uuid.FromString(c.Param("crop_id"))
	if err != nil {
		return Error(c, err)
	}

	noteUID, err := uuid.FromString(c.Param("note_id"))
	if err != nil {
		return Error(c, err)
	}

	// Validate //
	result := <-s.CropReadQuery.FindByID(cropUID)
	if result.Error != nil {
		return Error(c, result.Error)
	}

	cropRead, ok := result.Result.(storage.CropRead)
	if !ok {
		return Error(c, echo.NewHTTPError(http.StatusInternalServerError, "Internal server error"))
	}

	if cropRead.UID == (uuid.UUID{}) {
		return Error(c, NewRequestValidationError(NOT_FOUND, "id"))
	}

	// Process //
	eventQueryResult := <-s.CropEventQuery.FindAllByCropID(cropUID)
	if eventQueryResult.Error != nil {
		return Error(c, eventQueryResult.Error)
	}

	events := eventQueryResult.Result.([]storage.CropEvent)

	crop := repository.NewCropBatchFromHistory(events)

	err = crop.RemoveNote(noteUID)
	if err != nil {
		return Error(c, err)
	}

	// Persists //
	resultSave := <-s.CropEventRepo.Save(crop.UID, crop.Version, crop.UncommittedChanges)
	if resultSave != nil {
		return Error(c, echo.NewHTTPError(http.StatusInternalServerError, "Internal server error"))
	}

	// TRIGGER EVENTS //
	s.publishUncommittedEvents(crop)

	data := make(map[string]storage.CropRead)
	cr, err := MapToCropRead(s, *crop)
	if err != nil {
		return Error(c, err)
	}

	data["data"] = cr

	return c.JSON(http.StatusOK, data)
}

func (s *GrowthServer) FindAllCrops(c echo.Context) error {
	data := make(map[string][]storage.CropRead)

	// Params //
	farmID := c.Param("id")

	// Validate //
	farmUID, err := uuid.FromString(farmID)
	if err != nil {
		return Error(c, err)
	}

	result := <-s.FarmReadQuery.FindByID(farmUID)
	if result.Error != nil {
		return Error(c, result.Error)
	}

	farm, ok := result.Result.(query.CropFarmQueryResult)
	if !ok {
		return Error(c, echo.NewHTTPError(http.StatusInternalServerError, "Internal server error"))
	}

	// Process //
	resultQuery := <-s.CropReadQuery.FindAllCropsByFarm(farm.UID)
	if resultQuery.Error != nil {
		return Error(c, resultQuery.Error)
	}

	crops, ok := resultQuery.Result.([]storage.CropRead)
	if !ok {
		return Error(c, echo.NewHTTPError(http.StatusInternalServerError, "Internal server error"))
	}

	data["data"] = []storage.CropRead{}
	for _, v := range crops {
		data["data"] = append(data["data"], v)
	}

	return c.JSON(http.StatusOK, data)
}

func (s *GrowthServer) FindAllCropsByArea(c echo.Context) error {
	data := make(map[string][]CropListInArea)

	// Params //
	areaID := c.Param("id")

	// Validate //
	areaUID, err := uuid.FromString(areaID)
	if err != nil {
		return Error(c, err)
	}

	result := <-s.AreaReadQuery.FindByID(areaUID)
	if result.Error != nil {
		return Error(c, result.Error)
	}

	area, ok := result.Result.(query.CropAreaQueryResult)
	if !ok {
		return Error(c, echo.NewHTTPError(http.StatusInternalServerError, "Internal server error"))
	}

	// Process //
	resultQuery := <-s.CropReadQuery.FindAllCropsByArea(area.UID)
	if resultQuery.Error != nil {
		return Error(c, resultQuery.Error)
	}

	crops, ok := resultQuery.Result.([]query.CropAreaByAreaQueryResult)
	if !ok {
		return Error(c, echo.NewHTTPError(http.StatusInternalServerError, "Internal server error"))
	}

	data["data"] = []CropListInArea{}
	for _, v := range crops {
		cl, err := MapToCropListInArea(v)
		if err != nil {
			return Error(c, err)
		}
		data["data"] = append(data["data"], cl)
	}

	return c.JSON(http.StatusOK, data)
}

func (s *GrowthServer) UploadCropPhotos(c echo.Context) error {
	description := c.FormValue("description")
	cropUID, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return Error(c, err)
	}

	// Validate //
	photo, err := c.FormFile("photo")
	if err != nil {
		return Error(c, NewRequestValidationError(REQUIRED, "photo"))
	}

	result := <-s.CropReadQuery.FindByID(cropUID)
	if result.Error != nil {
		return Error(c, result.Error)
	}

	cropRead, ok := result.Result.(storage.CropRead)
	if !ok {
		return Error(c, echo.NewHTTPError(http.StatusBadRequest, "Internal server error"))
	}

	if cropRead.UID == (uuid.UUID{}) {
		return Error(c, NewRequestValidationError(NOT_FOUND, "id"))
	}

	// Process
	eventQueryResult := <-s.CropEventQuery.FindAllByCropID(cropUID)
	if eventQueryResult.Error != nil {
		return Error(c, eventQueryResult.Error)
	}

	events := eventQueryResult.Result.([]storage.CropEvent)

	crop := repository.NewCropBatchFromHistory(events)

	destPath := stringhelper.Join(*config.Config.UploadPathCrop, "/", photo.Filename)
	err = s.File.Upload(photo, destPath)
	if err != nil {
		return Error(c, err)
	}

	width, height, err := imagehelper.GetImageDimension(destPath)
	if err != nil {
		return Error(c, err)
	}

	err = crop.AddPhoto(
		photo.Filename,
		photo.Header["Content-Type"][0],
		int(photo.Size),
		width,
		height,
		description,
	)
	if err != nil {
		return Error(c, err)
	}

	// Persists //
	resultSave := <-s.CropEventRepo.Save(crop.UID, crop.Version, crop.UncommittedChanges)
	if resultSave != nil {
		return Error(c, echo.NewHTTPError(http.StatusInternalServerError, "Internal server error"))
	}

	// TRIGGER EVENTS //
	s.publishUncommittedEvents(crop)

	data := make(map[string]storage.CropRead)
	cr, err := MapToCropRead(s, *crop)
	if err != nil {
		return Error(c, err)
	}

	data["data"] = cr

	return c.JSON(http.StatusOK, data)
}

func (s *GrowthServer) GetCropPhotos(c echo.Context) error {
	cropUID, err := uuid.FromString(c.Param("crop_id"))
	if err != nil {
		return Error(c, err)
	}

	photoUID, err := uuid.FromString(c.Param("photo_id"))
	if err != nil {
		return Error(c, err)
	}

	// Validate //
	result := <-s.CropReadQuery.FindByID(cropUID)
	if result.Error != nil {
		return Error(c, result.Error)
	}

	cropRead, ok := result.Result.(storage.CropRead)
	if !ok {
		return Error(c, echo.NewHTTPError(http.StatusBadRequest, "Internal server error"))
	}

	if cropRead.UID == (uuid.UUID{}) {
		return Error(c, NewRequestValidationError(NOT_FOUND, "crop_id"))
	}

	found := storage.CropPhoto{}
	for _, v := range cropRead.Photos {
		if v.UID == photoUID {
			found = v
		}
	}

	if found == (storage.CropPhoto{}) {
		return Error(c, NewRequestValidationError(NOT_FOUND, "photo_id"))
	}

	// Process //
	srcPath := stringhelper.Join(*config.Config.UploadPathCrop, "/", found.Filename)

	return c.File(srcPath)
}

func (s *GrowthServer) GetCropActivities(c echo.Context) error {
	cropUID, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return Error(c, err)
	}

	result := <-s.CropReadQuery.FindByID(cropUID)
	if result.Error != nil {
		return Error(c, result.Error)
	}

	crop, ok := result.Result.(storage.CropRead)
	if !ok {
		return Error(c, echo.NewHTTPError(http.StatusBadRequest, "Internal server error"))
	}

	if crop.UID == (uuid.UUID{}) {
		return Error(c, NewRequestValidationError(NOT_FOUND, "id"))
	}

	// Process //
	queryResult := <-s.CropActivityQuery.FindAllByCropID(cropUID)
	if queryResult.Error != nil {
		return Error(c, queryResult.Error)
	}

	activities := queryResult.Result.([]storage.CropActivity)

	data := make(map[string][]CropActivity)
	for i := range activities {
		data["data"] = append(data["data"], MapToCropActivity(activities[i]))
	}

	return c.JSON(http.StatusOK, data)
}

func (s *GrowthServer) GetCropsInformation(c echo.Context) error {
	result := <-s.CropReadQuery.FindCropsInformation()
	if result.Error != nil {
		return Error(c, result.Error)
	}

	cropInf, ok := result.Result.(query.CropInformationQueryResult)
	if !ok {
		return Error(c, echo.NewHTTPError(http.StatusBadRequest, "Internal server error"))
	}

	data := make(map[string]query.CropInformationQueryResult)
	data["data"] = cropInf

	return c.JSON(http.StatusOK, data)
}

func (s *GrowthServer) publishUncommittedEvents(entity interface{}) error {
	switch e := entity.(type) {
	case *domain.Crop:
		for _, v := range e.UncommittedChanges {
			name := structhelper.GetName(v)
			s.EventBus.Publish(name, v)
		}
	}

	return nil
}
