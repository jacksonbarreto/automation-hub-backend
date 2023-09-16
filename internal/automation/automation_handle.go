package automation

import (
	"automation-hub-backend/internal/config"
	"automation-hub-backend/internal/model"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"io"
	"net/http"
)

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{
		service: service,
	}
}

func DefaultHandler() *Handler {
	return NewHandler(DefaultService())
}

func (h *Handler) ImageHandler(c *gin.Context) {
	imageName := c.Param("imageName")
	imagePath := config.AppConfig.ImageSaveDir + "/" + imageName

	c.File(imagePath)
}

// Create
// @Summary Create a new automation
// @Description Create a new automation with the input data
// @Tags Automations
// @Accept  json
// @Produce  json
// @Param automation body model.Automation true "Automation data"
// @Success 201 {object} model.Automation "Successfully created automation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /automations [post]
func (h *Handler) Create(c *gin.Context) {
	var automation model.Automation

	file, _ := c.FormFile("imageFile")
	if file != nil {
		automation.ImageFile = file
	}

	body, err := io.ReadAll(c.Request.Body)
	defer c.Request.Body.Close()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read request body"})
		return
	}

	if err := model.JSON.Unmarshal(body, &automation); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	newAutomation, err := h.service.Create(&automation)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, newAutomation)
}

// GetAll
// @Summary Get all automations
// @Description Retrieve all automations
// @Tags Automations
// @Accept  json
// @Produce  json
// @Success 200 {array} model.Automation "Successfully retrieved automations"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /automations [get]
func (h *Handler) GetAll(c *gin.Context) {
	automations, err := h.service.FindAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, automations)
}

// GetByID
// @Summary Get an automation by ID
// @Description Retrieve a specific automation by its ID
// @Tags Automations
// @Accept  json
// @Produce  json
// @Param id path string true "Automation ID"
// @Success 200 {object} model.Automation "Successfully retrieved automation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /automations/{id} [get]
func (h *Handler) GetByID(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
		return
	}

	automation, err := h.service.FindByID(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if automation == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Automation not found"})
		return
	}

	c.JSON(http.StatusOK, automation)
}

// DeleteByID
// @Summary Delete an automation by ID
// @Description Delete a specific automation by its ID
// @Tags Automations
// @Accept  json
// @Produce  json
// @Param id path string true "Automation ID"
// @Success 204 "Successfully deleted automation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /automations/{id} [delete]
func (h *Handler) DeleteByID(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
		return
	}

	err = h.service.Delete(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusNoContent)
}

// SwapPosition
// @Summary Swap positions of two automations
// @Description Swap the positions of two specific automations by their IDs
// @Tags Automations
// @Accept  json
// @Produce  json
// @Param id1 path string true "First Automation ID"
// @Param id2 path string true "Second Automation ID"
// @Success 200 "Successfully swapped positions"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /automations/{id1}/swap/{id2} [get]
func (h *Handler) SwapPosition(c *gin.Context) {
	id1Str := c.Param("id1")
	id2Str := c.Param("id2")

	id1, err := uuid.Parse(id1Str)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format for id1"})
		return
	}

	id2, err := uuid.Parse(id2Str)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format for id2"})
		return
	}

	err = h.service.SwapOrder(id1, id2)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusOK)
}

// Update
// @Summary Update an automation
// @Description Update a specific automation with the input data
// @Tags Automations
// @Accept  json
// @Produce  json
// @Param automation body model.Automation true "Automation data"
// @Success 200 {object} model.Automation "Successfully updated automation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /automations [patch]
func (h *Handler) Update(c *gin.Context) {
	var automation model.Automation

	body, err := io.ReadAll(c.Request.Body)
	defer c.Request.Body.Close()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read request body"})
		return
	}

	if err := model.JSON.Unmarshal(body, &automation); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updatedAutomation, err := h.service.Update(&automation)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, updatedAutomation)
}