package automation

import (
	"automation-hub-backend/internal/config"
	"automation-hub-backend/internal/events"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/util"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"image"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type Service interface {
	FindByID(id uuid.UUID) (*models.Automation, error)
	Create(automation *models.Automation) (*models.Automation, error)
	Update(automation *models.Automation) (*models.Automation, error)
	Delete(id uuid.UUID) error
	FindAll() ([]*models.Automation, error)
	SwapOrder(id1 uuid.UUID, id2 uuid.UUID) error
}

type service struct {
	repo      Repository
	publisher events.Publisher
}

func NewService(repo Repository, publisher events.Publisher) Service {
	return &service{
		repo:      repo,
		publisher: publisher,
	}
}

func DefaultService() Service {
	repo := DefaultRepository()
	pub := events.DefaultPublisher()
	return NewService(repo, *pub)
}

func (s *service) FindByID(id uuid.UUID) (*models.Automation, error) {
	return s.repo.FindByID(id)
}

func (s *service) Create(automation *models.Automation) (*models.Automation, error) {
	automation.ID = uuid.UUID{} // reset ID

	if automation.ImageFile != nil {
		file := automation.ImageFile
		fmt.Printf("Received file size: %d bytes\n", file.Size)
		fmt.Printf("Received file name: %s\n", file.Filename)
		tempFileName := "temp_test_file" + filepath.Ext(file.Filename)
		fullPath := config.AppConfig.ImageSaveDir + "/" + tempFileName
		fmt.Printf("Attempting to save file to: %s\n", fullPath)

		dst, err := os.Create(config.AppConfig.ImageSaveDir + "/" + tempFileName)
		if err != nil {
			fmt.Printf("Error creating file: %v\n", err)
		}
		defer dst.Close()

		src, err := file.Open()
		if err != nil {
			fmt.Printf("Error opening file: %v\n", err)
		}
		defer src.Close()

		_, err = io.Copy(dst, src)
		if err != nil {
			fmt.Printf("Error copying file: %v\n", err)
		}

		newFileName, err := s.processImageFile(automation.ImageFile)
		if err != nil {
			return nil, err
		}
		automation.Image = newFileName
	}

	maxPosition, err := s.repo.MaxPosition()
	if err != nil {
		return nil, err
	}
	automation.Position = maxPosition + 1

	err = s.ensureUniqueURLPath(automation)
	if err != nil {
		return nil, err
	}

	if err := automation.Validate(); err != nil {
		return nil, err
	}

	automationCreated, err := s.repo.Create(automation)
	if err != nil {
		return nil, err
	}
	event := &events.AutomationEvent{
		Type:       events.CreateEvent,
		Automation: automationCreated,
	}
	err = s.publisher.Publish(event)
	if err != nil {
		log.Printf("Failed to publish create event to Kafka: %v", err)
		return nil, err
	}
	return automationCreated, nil
}

func (s *service) Update(automation *models.Automation) (*models.Automation, error) {
	currentAutomation, err := s.repo.FindByID(automation.ID)
	if err != nil {
		return nil, err
	}

	automation.Position = currentAutomation.Position

	if automation.ImageFile != nil {
		newFileName, err := s.processImageFile(automation.ImageFile)
		if err != nil {
			return nil, err
		}
		if err := s.deleteImage(currentAutomation.Image); err != nil {
			return nil, err
		}
		automation.Image = newFileName
	} else if automation.RemoveImage {
		if err := s.deleteImage(currentAutomation.Image); err != nil {
			return nil, err
		}
		automation.Image = ""
	} else {
		automation.Image = currentAutomation.Image
	}

	if currentAutomation.Name != automation.Name {
		err = s.ensureUniqueURLPath(automation)
		if err != nil {
			return nil, err
		}
	} else {
		automation.URLPath = currentAutomation.URLPath
	}

	if err := automation.Validate(); err != nil {
		return nil, err
	}

	automationUpdated, err := s.repo.Update(automation)

	event := &events.AutomationEvent{
		Type:       events.UpdateEvent,
		Automation: automationUpdated,
	}

	err = s.publisher.Publish(event)
	if err != nil {
		log.Printf("Failed to publish update event to Kafka: %v", err)
		return nil, err
	}

	return automationUpdated, nil
}

func (s *service) Delete(id uuid.UUID) error {
	automation, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}

	err = s.repo.Delete(id)
	if err != nil {
		return err
	}

	event := &events.AutomationEvent{
		Type:       events.DeleteEvent,
		Automation: automation,
	}

	err = s.publisher.Publish(event)
	if err != nil {
		log.Printf("Failed to publish delete event to Kafka: %v", err)
		return err
	}

	return nil
}

func (s *service) FindAll() ([]*models.Automation, error) {
	return s.repo.FindAll()
}

func (s *service) SwapOrder(id1 uuid.UUID, id2 uuid.UUID) error {
	return s.repo.Transaction(func(tx *gorm.DB) error {
		automation1, err := s.repo.FindByID(id1)
		if err != nil {
			return err
		}
		automation2, err := s.repo.FindByID(id2)
		if err != nil {
			return err
		}

		pos1 := automation1.Position
		pos2 := automation2.Position

		maxPosition, err := s.repo.MaxPosition()
		if err != nil {
			return err
		}
		tempPosition := maxPosition + 1

		automation1.Position = tempPosition
		if err := tx.Save(automation1).Error; err != nil {
			return err
		}

		automation2.Position = pos1
		if err := tx.Save(automation2).Error; err != nil {
			return err
		}

		automation1.Position = pos2
		if err := tx.Save(automation1).Error; err != nil {
			return err
		}

		return nil
	})
}

func (s *service) processImageFile(file *multipart.FileHeader) (string, error) {
	if file.Size > config.AppConfig.ImageMaxSize {
		return "", fmt.Errorf("image is too large (%d). Max size is %d Mb", file.Size, config.AppConfig.ImageMaxSize)
	}

	ext := filepath.Ext(file.Filename)
	fmt.Printf("Filename: %s, Extracted Extension: %s\n", file.Filename, ext)

	if !contains(config.AppConfig.ImageExtensions, ext) {
		return "", fmt.Errorf("invalid image extension. Allowed extensions are: %v", config.AppConfig.ImageExtensions)
	}

	src, err := file.Open()
	if err != nil {
		return "", err
	}
	defer func(src multipart.File) {
		err := src.Close()
		if err != nil {
			fmt.Printf("Failed to close file: %v", err)
		}
	}(src)

	buffer := make([]byte, 512)
	_, err = src.Read(buffer)
	if err != nil {
		return "", err
	}
	fileType := http.DetectContentType(buffer)
	if !strings.HasPrefix(fileType, "image/") {
		return "", fmt.Errorf("file is not an image")
	}
	mimeSuffix := strings.TrimPrefix(fileType, "image/")
	if !contains(config.AppConfig.ImageExtensions, "."+mimeSuffix) {
		return "", fmt.Errorf("mismatch between file extension and MIME type")
	}

	_, err = src.Seek(0, 0)
	if err != nil {
		return "", err
	}

	_, _, err = image.Decode(src)
	if err != nil {
		//return "", fmt.Errorf("corrupted image: %v", err)
	}

	_, err = src.Seek(0, 0)
	if err != nil {
		return "", err
	}

	newFileName := uuid.New().String() + ext
	dst, err := os.Create(config.AppConfig.ImageSaveDir + "/" + newFileName)
	if err != nil {
		fmt.Printf("Failed to create file %s: %v", dst.Name(), err)
		return "", err
	}
	defer func(dst *os.File) {
		err := dst.Close()
		if err != nil {
			fmt.Printf("Failed to close file %s: %v", dst.Name(), err)
		}
	}(dst)
	fmt.Printf("Buffer content: %x\n", buffer[:100]) // Print first 100 bytes

	_, err = io.Copy(dst, src)
	if err != nil {
		return "", err
	}

	return newFileName, nil
}

func (s *service) deleteImage(imageName string) error {
	if imageName == "" {
		return nil
	}
	imagePath := config.AppConfig.ImageSaveDir + "/" + imageName
	if _, err := os.Stat(imagePath); os.IsNotExist(err) {
		return nil
	}
	return os.Remove(imagePath)
}

func contains(slice []string, str string) bool {
	str = strings.ToLower(str)
	for _, v := range slice {
		if v == str {
			return true
		}
	}
	return false
}

func (s *service) ensureUniqueURLPath(automation *models.Automation) error {
	baseURLPath := util.GenerateURLPath(automation.Name)
	uniqueURLPath := baseURLPath
	counter := 0

	for {
		existingAutomation, err := s.repo.GetByURLPath(uniqueURLPath)
		if err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}

		if existingAutomation == nil || existingAutomation.ID == automation.ID {
			break
		}

		counter++
		uniqueURLPath = fmt.Sprintf("%s-%d", baseURLPath, counter)
	}

	automation.URLPath = uniqueURLPath
	return nil
}
